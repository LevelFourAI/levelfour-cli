package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const (
	usageBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","period":"2026-09",` +
		`"start_date":"2026-09-01","end_date":"2026-09-30","total_period_cost":744,"measure":"usage","unit":"hours",` +
		`"items":[{"service":"Amazon EC2","usage_type":"BoxUsage:m5.large","cost":720},` +
		`{"service":"Amazon EC2","usage_type":"BoxUsage:t3.micro","cost":24}],` +
		`"pagination":{"total_items":2,"total_pages":1,"current_page":1,"page_size":20,"has_next":false,"has_previous":false}}}`

	noUsageBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","period":"2026-09",` +
		`"start_date":"2026-09-01","end_date":"2026-09-30","total_period_cost":0,"measure":"usage","unit":"gb","items":[]}}`

	// What an API that predates usage answers: the same table, measured in dollars.
	costBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","period":"2026-09",` +
		`"start_date":"2026-09-01","end_date":"2026-09-30","total_period_cost":744,` +
		`"items":[{"service":"Amazon EC2","usage_type":"BoxUsage:m5.large","cost":720}],` +
		`"pagination":{"total_items":1,"total_pages":1,"current_page":1,"page_size":20,"has_next":false,"has_previous":false}}}`

	forecastBody = `{"success":true,"data":{"provider_id":"gcp","provider_name":"Google Cloud","granularity":"monthly",` +
		`"groups":["proj-1","__other__","gone"],"group_labels":{"proj-1":"Checkout"},"data_points":[],"totals":{},` +
		`"data_complete_through":"2026-08","measure":"cost","unit":null,` +
		`"forecast":{"start_date":"2026-09","end_date":"2026-10","data_points":[` +
		`{"date":"2026-09","value":300,"low":280,"high":330.5,"groups":{"proj-1":{"value":200,"low":190,"high":220},"__other__":{"value":100,"low":90,"high":110.5}}},` +
		`{"date":"2026-10","value":310,"low":290,"high":340,"groups":{"proj-1":{"value":210,"low":200,"high":230},"__other__":{"value":100,"low":90,"high":110}}}]}}}`

	usageForecastBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","granularity":"daily",` +
		`"groups":["Amazon EC2"],"data_points":[],"totals":{},"data_complete_through":"2026-09-27","measure":"usage","unit":"hours",` +
		`"forecast":{"start_date":"2026-09-28","end_date":"2026-09-28","data_points":[` +
		`{"date":"2026-09-28","value":24,"low":20,"high":26,"groups":{"Amazon EC2":{"value":24,"low":20,"high":26}}}]}}}`

	nothingToProjectBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","granularity":"monthly",` +
		`"groups":[],"data_points":[],"totals":{},"data_complete_through":null,"forecast":null}}`

	// What an API that predates the projection answers: the chart alone.
	chartBody = `{"success":true,"data":{"provider_id":"aws","provider_name":"AWS","granularity":"monthly",` +
		`"groups":[],"data_points":[],"totals":{}}}`
)

func serveBreakdown(t *testing.T, status int, body string) *url.Values {
	t.Helper()
	seen := &url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/costs/breakdown") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		*seen = r.URL.Query()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	return seen
}

func breakdown(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runCLI(t, append([]string{"costs", "breakdown", "--provider", "aws"}, args...)...)
}

func TestCostsBreakdownMeasuredAsUsage(t *testing.T) {
	seen := serveBreakdown(t, http.StatusOK, usageBody)

	out, err := breakdown(t, "--measure", "usage", "--unit", "hours", "--group-by", "usage_type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "744.00 hours", "Usage (hours)", "Usage Type", "BoxUsage:m5.large", "720.00", "24.00")
	assertNotContains(t, out, "$", "Change", "Prev Cost")
	for param, want := range map[string]string{"format": "table", "measure": "usage", "unit": "hours", "group_by": "usage_type"} {
		if got := seen.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestCostsBreakdownSaysWhenNoUsageIsInThatUnit(t *testing.T) {
	serveBreakdown(t, http.StatusOK, noUsageBody)

	out, err := breakdown(t, "--measure", "usage", "--unit", "gb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "No usage measured in gb", "not converted")
}

// Dollars printed under a unit would be a wrong answer that looks right.
func TestCostsBreakdownRefusesCostsAnsweredForUsage(t *testing.T) {
	serveBreakdown(t, http.StatusOK, costBody)

	out, err := breakdown(t, "--measure", "usage", "--unit", "hours")
	if err == nil || !strings.Contains(err.Error(), "does not measure usage yet") {
		t.Fatalf("error = %v, want a refusal to print costs as usage", err)
	}
	assertNotContains(t, out, "720")
}

// A script asked for hours too: the raw answer is held to the same check as the table.
func TestCostsBreakdownRefusesCostsAnsweredForUsageUnderFormattingFlags(t *testing.T) {
	serveBreakdown(t, http.StatusOK, costBody)

	out, err := breakdown(t, "--measure", "usage", "--unit", "hours", "--jq", ".data.total_period_cost")
	if err == nil || !strings.Contains(err.Error(), "does not measure usage yet") {
		t.Fatalf("error = %v, want a refusal to hand costs to a script asking for usage", err)
	}
	assertNotContains(t, out, "744")
}

func TestCostsBreakdownForecastRefusesAnAnswerWithoutOneUnderFormattingFlags(t *testing.T) {
	serveBreakdown(t, http.StatusOK, chartBody)

	if _, err := breakdown(t, "--forecast", "1m", "--json"); err == nil || !strings.Contains(err.Error(), "does not project one yet") {
		t.Fatalf("error = %v, want a refusal to print a chart with no forecast as one", err)
	}
}

// Zero would read as a real lower bound.
func TestCostsBreakdownForecastPrintsNoBoundTheAnswerLeftOut(t *testing.T) {
	serveBreakdown(t, http.StatusOK, `{"success":true,"data":{"provider_name":"AWS","groups":[],`+
		`"data_complete_through":"2026-08","forecast":{"data_points":[{"date":"2026-09","value":300,"groups":{}}]}}}`)

	out, err := breakdown(t, "--forecast", "1m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "$300.00")
	assertNotContains(t, out, "$0.00")
}

func TestCostsBreakdownUsageHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveBreakdown(t, http.StatusOK, usageBody)

	out, err := breakdown(t, "--measure", "usage", "--unit", "hours", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, `"unit":"hours"`)
}

func TestCostsBreakdownUsageFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"the API refuses the unit": {
			http.StatusUnprocessableEntity,
			`{"success":false,"error":{"message":"Unknown unit: miles. Usage is measured in hours, gb."}}`,
			"Usage is measured in hours, gb.",
		},
		"the answer is not JSON":  {http.StatusOK, `<html>`, "unexpected response"},
		"the answer has no table": {http.StatusOK, `{"success":true,"data":null}`, "unexpected response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			serveBreakdown(t, tc.status, tc.body)
			_, err := breakdown(t, "--measure", "usage", "--unit", "miles")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one saying %q", err, tc.want)
			}
		})
	}
}

func TestCostsBreakdownUsageFailsWhenTheAPIIsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"

	if _, err := breakdown(t, "--measure", "usage", "--unit", "hours"); err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestCostsBreakdownGroupedByUsageTypeNamesIt(t *testing.T) {
	seen := serveBreakdown(t, http.StatusOK, costBody)

	out, err := breakdown(t, "--group-by", "usage_type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Usage Type", "BoxUsage:m5.large", "$720.00")
	if got := seen.Get("group_by"); got != "usage_type" {
		t.Errorf("group_by = %q, want usage_type", got)
	}
	if seen.Has("measure") {
		t.Errorf("query = %v, want no measure on a cost breakdown", *seen)
	}
}

func TestCostsBreakdownRejectsFlagsThatDoNotGoTogether(t *testing.T) {
	cases := map[string]struct {
		args []string
		want string
	}{
		"a measure it does not have":   {[]string{"--measure", "hours"}, "--measure must be cost or usage"},
		"a unit for a cost":            {[]string{"--unit", "hours"}, "--unit needs --measure usage"},
		"a unit beside --measure cost": {[]string{"--measure", "cost", "--unit", "hours"}, "--unit needs --measure usage"},
		"usage with no unit":           {[]string{"--measure", "usage"}, "--unit is required"},
		"usage as csv":                 {[]string{"--measure", "usage", "--unit", "hours", "--format", "csv"}, "cannot be combined with --format csv"},
		"usage beside a virtual tag": {
			[]string{"--measure", "usage", "--unit", "hours", "--virtual-tag-key", "Teams"},
			"cannot be combined with --virtual-tag-key",
		},
		"usage in the viewer":         {[]string{"--measure", "usage", "--unit", "hours", "--tui"}, "--measure usage cannot be combined with --tui"},
		"a horizon it does not offer": {[]string{"--forecast", "6m"}, "--forecast must be 1m or 3m"},
		"a forecast as raw rows":      {[]string{"--forecast", "1m", "--format", "raw"}, "cannot be combined with --format raw"},
		"a forecast in the viewer":    {[]string{"--forecast", "1m", "--tui"}, "--forecast cannot be combined with --tui"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			seen := serveBreakdown(t, http.StatusOK, usageBody)
			_, err := breakdown(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one saying %q", err, tc.want)
			}
			if len(*seen) != 0 {
				t.Errorf("the API was asked %v, want nothing sent", *seen)
			}
		})
	}
}

func TestCostsBreakdownForecastIsMonthlyUnlessAsked(t *testing.T) {
	seen := serveBreakdown(t, http.StatusOK, forecastBody)

	out, err := breakdown(t, "--forecast", "1m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Google Cloud", "Complete through", "2026-08",
		"2026-09", "Total", "$300.00", "$280.00", "$330.50",
		"Checkout", "$200.00", "Other", "$110.50", "2026-10", "$310.00", "median daily rate")
	assertNotContains(t, out, "proj-1", "__other__", "gone", "Measured in")
	for param, want := range map[string]string{"format": "chart", "forecast_horizon": "1m", "granularity": "monthly"} {
		if got := seen.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestCostsBreakdownForecastOfUsageByDay(t *testing.T) {
	seen := serveBreakdown(t, http.StatusOK, usageForecastBody)

	out, err := breakdown(t, "--forecast", "3m", "--granularity", "daily", "--measure", "usage", "--unit", "hours")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Measured in", "hours", "2026-09-28", "Amazon EC2", "24.00", "20.00", "26.00")
	assertNotContains(t, out, "$")
	if seen.Get("granularity") != "daily" || seen.Get("measure") != "usage" || seen.Get("unit") != "hours" {
		t.Errorf("query = %v, want the day buckets and the usage measure", *seen)
	}
}

// The projection exists and the usage measure does not: the figures are dollars.
func TestCostsBreakdownForecastRefusesCostsAnsweredForUsage(t *testing.T) {
	serveBreakdown(t, http.StatusOK, forecastBody)

	out, err := breakdown(t, "--forecast", "1m", "--measure", "usage", "--unit", "hours")
	if err == nil || !strings.Contains(err.Error(), "does not measure usage yet") {
		t.Fatalf("error = %v, want a refusal to print costs as usage", err)
	}
	assertNotContains(t, out, "300.00")
}

func TestCostsBreakdownKeepsUsageTypeOutOfAnUngroupedTable(t *testing.T) {
	if got := columnHeaders(activeCostColumns(400, nil)); strings.Contains(got, columnUsageType) {
		t.Errorf("columns = %s, want no Usage Type on a breakdown not grouped by it", got)
	}
	if got := columnHeaders(activeCostColumns(80, []string{dimUsageType})); !strings.Contains(got, columnUsageType) {
		t.Errorf("columns = %s, want Usage Type when grouped by it", got)
	}
}

func columnHeaders(columns []costColumn) string {
	headers := make([]string, 0, len(columns))
	for _, column := range columns {
		headers = append(headers, column.header)
	}
	return strings.Join(headers, "|")
}

func TestCostsBreakdownForecastSaysWhenThereIsNothingToProject(t *testing.T) {
	serveBreakdown(t, http.StatusOK, nothingToProjectBody)

	out, err := breakdown(t, "--forecast", "1m")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "Nothing to project from")
	assertNotContains(t, out, "Complete through")
}

func TestCostsBreakdownForecastHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveBreakdown(t, http.StatusOK, forecastBody)

	out, err := breakdown(t, "--forecast", "1m", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, `"data_complete_through":"2026-08"`)
}

func TestCostsBreakdownForecastFailures(t *testing.T) {
	cases := map[string]struct {
		status int
		body   string
		want   string
	}{
		"the API predates the projection": {http.StatusOK, chartBody, "does not project one yet"},
		"the API refuses the request": {
			http.StatusBadRequest, `{"success":false,"error":{"message":"Invalid date format"}}`, "Invalid date format",
		},
		"the answer is not JSON":  {http.StatusOK, `<html>`, "unexpected response"},
		"the answer has no chart": {http.StatusOK, `{"success":true,"data":null}`, "unexpected response"},
		"the forecast is not one": {http.StatusOK, `{"success":true,"data":{"forecast":"soon"}}`, "unexpected response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			serveBreakdown(t, tc.status, tc.body)
			_, err := breakdown(t, "--forecast", "1m")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one saying %q", err, tc.want)
			}
		})
	}
}
