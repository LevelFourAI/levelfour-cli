package cli

import (
	"net/http"
	"strings"
	"testing"
)

const (
	reportsRoute = "GET /api/v1/reports"
	byRegionID   = "0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e"
	byRegionPath = "GET /api/v1/reports/" + byRegionID
	byRegionRun  = byRegionPath + "/data"

	reportsJSON = `[
  {"id":"0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e","name":"EC2 by region","scope":"aws","created_by_name":"Ana","updated_at":"2026-09-28T09:00:00Z"},
  {"id":"7c6b5a49-3827-4615-8f4e-3d2c1b0a9f8e","name":"Compute hours","scope":"all","created_by_name":null,"updated_at":"2026-09-29T09:00:00Z"}
]`

	byRegionJSON = `{"id":"0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e","name":"EC2 by region","scope":"all","created_by_name":"Ana",` +
		`"updated_at":"2026-09-28T09:00:00Z","requests":{"chart":[],"table":[` +
		`{"provider_id":"aws","group_by":["service","region"],"filters":{"service":["Amazon EC2"],"account_id":["1"]},"measure":"cost","unit":null},` +
		`{"provider_id":"gcp","group_by":[],"filters":{},"measure":"usage","unit":"hours"}]}}`

	chartOnlyJSON = `{"id":"0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e","name":"EC2 by region","scope":"aws",` +
		`"requests":{"chart":[{"provider_id":"aws","group_by":["region"],"filters":{},"measure":"cost"}],"table":[]}}`

	unrunnableJSON = `{"id":"0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e","name":"EC2 by region","scope":"aws","requests":null}`

	runJSON = `{"report_id":"0b9d7c1e-5a3f-4e2b-8c6d-1f0a2b3c4d5e","name":"EC2 by region","format":"table","providers":[
  {"provider_id":"aws","table":{"provider_id":"aws","provider_name":"AWS","start_date":"2026-09-01","end_date":"2026-09-30",
    "total_period_cost":140.5,"measure":"cost","unit":null,
    "items":[{"service":"Amazon EC2","region":"us-east-1","cost":100.5},{"service":"Amazon EC2","region":null,"cost":40},
      {"service":"Amazon S3","region":"sa-east-1"}],
    "pagination":{"total_items":7,"total_pages":4,"current_page":1,"page_size":2,"has_next":true,"has_previous":false}}},
  {"provider_id":"gcp","table":{"provider_id":"gcp","provider_name":"Google Cloud","start_date":"2026-09-01","end_date":"2026-09-30",
    "total_period_cost":24,"measure":"usage","unit":"hours",
    "items":[{"usage_type":"N2 Instance Core","cost":24}],
    "pagination":{"total_items":1,"total_pages":1,"current_page":1,"page_size":2,"has_next":false,"has_previous":false}}},
  {"provider_id":"azure","error":{"code":"provider_not_connected","message":"azure is not connected."}},
  {"provider_id":"digitalocean","table":{"provider_id":"digitalocean","provider_name":"DigitalOcean","start_date":"2026-09-01",
    "end_date":"2026-09-30","total_period_cost":0,"measure":"cost","items":[],
    "pagination":{"total_items":0,"total_pages":0,"current_page":1,"page_size":2,"has_next":false,"has_previous":false}}}
]}`
)

func TestReportsListNamesEachReportsScope(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(reportsJSON)})

	out, err := runCLI(t, "reports", "list", "--scope", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "EC2 by region", byRegionID, "aws", "Ana", "2026-09-28", "Compute hours")
	req, _ := srv.last(http.MethodGet, "/api/v1/reports")
	if got := req.query.Get("scope"); got != "aws" {
		t.Errorf("scope = %q, want aws", got)
	}
}

func TestReportsListSaysWhereReportsComeFrom(t *testing.T) {
	serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(`[]`)})

	out, err := runCLI(t, "reports", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "saved from a board")
}

func TestReportsListHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(reportsJSON)})

	out, err := runCLI(t, "reports", "list", "--jq", ".data[1].scope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "all")
	assertNotContains(t, out, "EC2 by region")
}

func TestReportsListFailsWithTheAPIsReason(t *testing.T) {
	serveTags(t, map[string]tagsRoute{reportsRoute: errRoute(http.StatusForbidden, `{"message":"Reports are not part of this plan"}`)})

	_, err := runCLI(t, "reports", "list")
	if err == nil || !strings.Contains(err.Error(), "not part of this plan") {
		t.Fatalf("error = %v, want the API's reason", err)
	}
}

func TestReportsListRejectsAnAnswerItCannotRead(t *testing.T) {
	serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(`{"reports":[]}`)})

	if _, err := runCLI(t, "reports", "list"); err == nil {
		t.Fatal("expected an error for a list that is not one")
	}
}

func TestReportsGetShowsWhatEachProviderIsAsked(t *testing.T) {
	serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(reportsJSON), byRegionPath: okRoute(byRegionJSON)})

	out, err := runCLI(t, "reports", "get", "ec2 BY region")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "EC2 by region", "Ana", "2026-09-28",
		"AWS", "cost", "service, region", "account_id, service",
		"Google Cloud", "usage (hours)")
}

func TestReportsGetFallsBackToTheChartRequests(t *testing.T) {
	serveTags(t, map[string]tagsRoute{byRegionPath: okRoute(chartOnlyJSON)})

	out, err := runCLI(t, "reports", "get", byRegionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "AWS", "region")
}

func TestReportsGetSaysWhenAReportCannotRun(t *testing.T) {
	serveTags(t, map[string]tagsRoute{byRegionPath: okRoute(unrunnableJSON)})

	out, err := runCLI(t, "reports", "get", byRegionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "save it again in the dashboard")
}

func TestReportsGetHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveTags(t, map[string]tagsRoute{byRegionPath: okRoute(byRegionJSON)})

	out, err := runCLI(t, "reports", "get", byRegionID, "--jq", ".data.requests.table[1].unit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "hours")
}

func TestReportsGetFailures(t *testing.T) {
	cases := map[string]struct {
		routes map[string]tagsRoute
		ref    string
	}{
		"the name lookup fails":       {map[string]tagsRoute{}, "EC2 by region"},
		"no report has that name":     {map[string]tagsRoute{reportsRoute: okRoute(reportsJSON)}, "Lambda"},
		"no report has that id":       {map[string]tagsRoute{}, byRegionID},
		"the report is not an object": {map[string]tagsRoute{byRegionPath: okRoute(`[]`)}, byRegionID},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			serveTags(t, tc.routes)
			if _, err := runCLI(t, "reports", "get", tc.ref); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestReportsRunPrintsEachProvidersAnswer(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{byRegionRun: okRoute(runJSON)})

	out, err := runCLI(t, "reports", "run", byRegionID, "--start", "2026-09-01", "--end", "2026-09-30", "--page-size", "2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out,
		"AWS", "$140.50", "2026-09-01", "Amazon EC2", "us-east-1", "$100.50", "$40.00", "Page 1 of 4", "--page 2",
		"Amazon S3", "sa-east-1",
		"Google Cloud", "24.00 hours", "Usage (hours)", "N2 Instance Core",
		"Azure", "azure is not connected.",
		"DigitalOcean", "Nothing was billed in that window.")
	assertNotContains(t, out, "$24.00")
	req, _ := srv.last(http.MethodGet, "/api/v1/reports/"+byRegionID+"/data")
	for param, want := range map[string]string{
		"format": "table", "start": "2026-09-01", "end": "2026-09-30", "page": "1", "page_size": "2",
	} {
		if got := req.query.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestReportsRunByNameOverAPreset(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{reportsRoute: okRoute(reportsJSON), byRegionRun: okRoute(runJSON)})

	if _, err := runCLI(t, "reports", "run", "EC2 by region", "--preset", "30D", "--page", "3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	req, _ := srv.last(http.MethodGet, "/api/v1/reports/"+byRegionID+"/data")
	if req.query.Get("preset") != "30D" || req.query.Get("page") != "3" || req.query.Get("page_size") != "50" {
		t.Errorf("query = %v, want the preset, page 3 and the default page size", req.query)
	}
}

func TestReportsRunSaysWhenAReportCannotRun(t *testing.T) {
	serveTags(t, map[string]tagsRoute{byRegionRun: okRoute(`{"report_id":"x","name":"EC2 by region","format":"table","providers":[]}`)})

	out, err := runCLI(t, "reports", "run", byRegionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "save it again in the dashboard")
}

func TestReportsRunHandsFormattingFlagsTheWholeAnswer(t *testing.T) {
	serveTags(t, map[string]tagsRoute{byRegionRun: okRoute(runJSON)})

	out, err := runCLI(t, "reports", "run", byRegionID, "--jq", ".data.providers[0].table.total_period_cost")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out, "140.5")
}

func TestReportsRunFailures(t *testing.T) {
	served := map[string]tagsRoute{byRegionRun: okRoute(runJSON)}
	cases := map[string]struct {
		routes map[string]tagsRoute
		args   []string
		want   string
	}{
		"a page before the first":    {served, []string{byRegionID, "--page", "0"}, "--page must be 1 or more"},
		"a page of no rows":          {served, []string{byRegionID, "--page-size", "0"}, "--page-size must be between 1 and 100"},
		"a page past the most rows":  {served, []string{byRegionID, "--page-size", "500"}, "--page-size must be between 1 and 100"},
		"a preset it does not offer": {served, []string{byRegionID, "--preset", "7D"}, "--preset must be one of"},
		"a start that is not a day":  {served, []string{byRegionID, "--start", "September"}, "use YYYY-MM-DD"},
		"an end before its start":    {served, []string{byRegionID, "--start", "2026-09-30", "--end", "2026-09-01"}, "is before --start"},
		"no report has that name":    {map[string]tagsRoute{reportsRoute: okRoute(reportsJSON)}, []string{"Lambda"}, "l4 reports list"},
		"the run is refused":         {map[string]tagsRoute{byRegionRun: errRoute(http.StatusBadRequest, `{"message":"Invalid date format"}`)}, []string{byRegionID}, "Invalid date format"},
		"the run is not an object":   {map[string]tagsRoute{byRegionRun: okRoute(`[]`)}, []string{byRegionID}, "unexpected response"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			serveTags(t, tc.routes)
			_, err := runCLI(t, append([]string{"reports", "run"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one saying %q", err, tc.want)
			}
		})
	}
}

// A zero there would state a cost the API never sent.
func TestReportRowWithNoFigurePrintsNone(t *testing.T) {
	sent := map[string]interface{}{"service": "Amazon EC2", "cost": 12.5}
	missing := map[string]interface{}{"service": "Amazon S3"}

	if got := rowAmount(sent, measured{}); got != "$12.50" {
		t.Errorf("rowAmount(sent) = %q, want $12.50", got)
	}
	if got := rowAmount(missing, measured{}); got != "-" {
		t.Errorf("rowAmount(missing) = %q, want -", got)
	}
}

func TestReportTotalTheRunLeftOutPrintsNone(t *testing.T) {
	total := 12.5

	if got := totalSent(&total, measured{}); got != "$12.50" {
		t.Errorf("totalSent(sent) = %q, want $12.50", got)
	}
	if got := totalSent(nil, measured{measure: measureUsage, unit: "hours"}); got != "-" {
		t.Errorf("totalSent(missing) = %q, want -", got)
	}
}

func TestMeasuredFigures(t *testing.T) {
	cost, usage := measured{}, measured{measure: measureUsage, unit: "gb"}

	if got := []string{cost.header(), cost.amount(12.5), cost.total(12.5)}; strings.Join(got, "|") != "Cost|$12.50|$12.50" {
		t.Errorf("cost figures = %v", got)
	}
	if got := []string{usage.header(), usage.amount(12.5), usage.total(12.5)}; strings.Join(got, "|") != "Usage (gb)|12.50|12.50 gb" {
		t.Errorf("usage figures = %v", got)
	}
}
