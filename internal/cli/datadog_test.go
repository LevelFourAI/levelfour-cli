package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	kr "github.com/zalando/go-keyring"
)

const (
	datadogRenewalBody = `{"data":{"as_of":"2026-11-04","volume_in_force":false,"products":[` +
		`{"product":"apm_host","label":"APM Hosts","unit":"hosts","usage":null,"committed_quantity":null,` +
		`"quantity_note":null,"used_pct":null,"unused_commitment_usd":null,"on_demand_usd":100,"paid_usd":100,` +
		`"effective_rate_usd":null,"list_rate_usd":null,"list_tier":null,"rate_per":1,"rate_vs_list_pct":null,` +
		`"open_savings_monthly_usd":0,"usage_trend_pct":null,"recommended":null,"recommended_after_findings":null},` +
		`{"product":"infra_host","label":"Infra Hosts","unit":"hosts",` +
		`"usage":{"min":10,"median":40,"p95":50,"latest":45},"committed_quantity":30,` +
		`"quantity_note":"Read off the bill","used_pct":92.5,"unused_commitment_usd":12,"on_demand_usd":900,` +
		`"paid_usd":9000,"effective_rate_usd":23,"list_rate_usd":23,"list_tier":"Enterprise","rate_per":1,` +
		`"rate_vs_list_pct":0,"open_savings_monthly_usd":75,"usage_trend_pct":4.2,` +
		`"recommended":{"quantity":40,"annual_usd":8800,"price_basis":"current"},` +
		`"recommended_after_findings":{"quantity":0,"annual_usd":8000,"price_basis":"current"}}]}}`

	datadogPlanBody = `{"data":{"as_of":"2026-11-04","last_12_usd":14400,"monthly_now_usd":1250,"term_months":24,` +
		`"points":[` +
		`{"label":"Infra Hosts","rule":"grew","usage_median":40,"usage_now":60,"monthly_usd":500,"term_usd":12000},` +
		`{"label":"Logs","rule":"above_contract","usage_median":null,"usage_now":null,"monthly_usd":40,"term_usd":960},` +
		`{"label":"RUM","rule":"twice","usage_median":1,"usage_now":1,"monthly_usd":10,"term_usd":240},` +
		`{"label":"New","rule":"a_new_rule","usage_median":1,"usage_now":1,"monthly_usd":1,"term_usd":24}]}}`

	datadogContractBody = `{"data":{"month":"2026-11-01","contracts":[{"id":"ctr-1","lines":[` +
		`{"dimension":"infra_host","unit":"hosts","committed":100,"month_to_date":90,"projected_month_end":120,` +
		`"projected_overage":20,"crossing_day":"2026-11-20","projection_note":null},` +
		`{"dimension":"logs","unit":"USD","committed":50,"month_to_date":1,"projected_month_end":null,` +
		`"projected_overage":null,"crossing_day":null,"projection_note":"Too early in the month"}]}]}}`

	datadogHygieneBody = `{"data":{"score":87,"monthly_spend":1250,"rules":[` +
		`{"label":"Unused custom metrics","passed":false,"finding_count":3,"monthly_savings":450,"bill":"datadog"},` +
		`{"label":"Idle hosts","passed":true,"finding_count":0,"monthly_savings":0,"bill":"aws"}]}}`

	datadogMonthsBody = `{"data":{"as_of":"2026-11-04",` +
		`"months":[{"month":"2026-09-01","state":"final"},{"month":"2026-10-01","state":"estimated"},` +
		`{"month":"2026-11-01","state":"month_to_date"}],` +
		`"products":[{"product":"infra_host","label":"Infra Hosts","unit":"hosts","months":[` +
		`{"month":"2026-09-01","usage":40,"cost_usd":900},{"month":"2026-10-01","usage":45,"cost_usd":950},` +
		`{"month":"2026-11-01","usage":null,"cost_usd":null}]}]}}`

	datadogAWSBody = `{"data":{"seller":null,"billed_via":"direct","months":[` +
		`{"month":"2026-10-01","aws_billed_usd":100,"datadog_reported_usd":90,"difference_usd":10},` +
		`{"month":"2026-09-01","aws_billed_usd":null,"datadog_reported_usd":90,"difference_usd":null}]}}`

	datadogHostsBody = `{"data":{"total":3,"page":1,"page_size":2,"partial":true,"reason":null,` +
		`"facets":{"cloud":[{"value":"aws","count":3}],"team":[]},"items":[` +
		`{"id":"c:web-1","name":"web-1","cloud":"aws","team":null,"up":true,"last_reported_at":"2026-11-04T10:00:00Z",` +
		`"estimated_monthly_usd":23},` +
		`{"id":"c:web-2","name":"web-2","cloud":"aws","team":"core","up":false,"last_reported_at":null,` +
		`"estimated_monthly_usd":null}]}}`

	datadogHostBody = `{"data":{"id":"c:web-1","name":"web-1","cloud":"aws","team":null,"up":true,` +
		`"estimated_monthly_usd":23,"recommendations":[` +
		`{"id":"DD-1","monthly_savings":40,"detector":"datadog_agentless_host"}],` +
		`"detail":{"changes":[{"at":"2026-11-01","actor":"someone@example.com","action":"modified"}]}}}`

	datadogBareHostBody = `{"data":{"id":"c:web-2","name":"web-2","recommendations":[],"detail":{"changes":[]}}}`

	datadogQuotasBody = `{"data":{"total":1,"page":1,"page_size":50,"partial":false,"facets":{"has_quota":[]},"items":[` +
		`{"id":"c:main","name":"main","daily_limit":1000,"daily_events":250,"quota_used_share":0.25,` +
		`"monthly_events":7500,"estimated_monthly_usd":12.5},` +
		`{"id":"c:off","name":"off","daily_limit":null,"daily_events":null,"quota_used_share":null,` +
		`"monthly_events":null,"estimated_monthly_usd":null}]}}`

	datadogEmptyBody = `{"data":{"total":0,"page":1,"page_size":50,"partial":false,"facets":{},"items":[],"reason":null}}`

	datadogReasonBody = `{"data":{"total":0,"page":1,"page_size":50,"partial":false,"facets":{},"items":[],` +
		`"reason":"No AWS bill reaches that month"}}`

	datadogNoPageSizeBody = `{"data":{"total":1,"page":1,"page_size":0,"partial":false,"facets":{},"items":[` +
		`{"product":"infra_host"}]}}`

	datadogCSVBody = "product,label\ninfra_host,Infra Hosts\n"
)

// datadogServer answers each route with its body, and a tenant without Datadog
// the way the API does when a route has no body here.
func datadogServer(t *testing.T, bodies map[string]string) *[]string {
	t.Helper()
	asked := &[]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*asked = append(*asked, r.URL.RawQuery)
		route := strings.TrimPrefix(r.URL.Path, datadogAPIPath)
		if r.URL.Query().Get("format") == formatCSV {
			route += ".csv"
		}
		body, ok := bodies[route]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"detail":{"code":"provider_not_enabled"}}`)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)
	return asked
}

func runDatadog(t *testing.T, bodies map[string]string, args ...string) (string, error) {
	t.Helper()
	datadogServer(t, bodies)
	out, _, err := executeCommand(t, append([]string{"providers", "datadog"}, args...)...)
	return out.String(), err
}

func assertShows(t *testing.T, out string, wanted ...string) {
	t.Helper()
	for _, want := range wanted {
		if !strings.Contains(out, want) {
			t.Errorf("output should show %q:\n%s", want, out)
		}
	}
}

func TestDatadogRenewalOpensOnTheLargestProduct(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal": datadogRenewalBody}, "renewal")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	if strings.Index(out, "Infra Hosts") > strings.Index(out, "APM Hosts") {
		t.Errorf("the product that paid the most should come first:\n%s", out)
	}
	assertShows(t, out, "$9000.00", "40 hosts", "$8800.00", notMeasured, "potential")
}

func TestDatadogRenewalOneProductInFull(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal": datadogRenewalBody}, "renewal", "infra_host")
	if err != nil {
		t.Fatalf("renewal infra_host error: %v", err)
	}
	assertShows(t, out, "Infra Hosts", "$23.00 per 1", "Enterprise", "stay on demand", "Read off the bill", "92.5%")
	if strings.Contains(out, "APM Hosts") {
		t.Errorf("only the product asked for should show:\n%s", out)
	}
}

func TestDatadogRenewalProductWithNothingMeasuredSaysSo(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal": datadogRenewalBody}, "renewal", "apm_host")
	if err != nil {
		t.Fatalf("renewal apm_host error: %v", err)
	}
	assertShows(t, out, "APM Hosts", notMeasured)
}

func TestDatadogRenewalRefusesAProductTheBillDoesNotHold(t *testing.T) {
	if _, err := runDatadog(t, map[string]string{"/renewal": datadogRenewalBody}, "renewal", "nope"); err == nil {
		t.Error("expected an error for a product the bill does not hold")
	}
}

func TestDatadogRenewalWithNoProducts(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal": `{"data":{"products":[]}}`}, "renewal")
	if err != nil {
		t.Fatalf("renewal error: %v", err)
	}
	assertShows(t, out, "no closed month")
}

func TestDatadogRenewalCSV(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal.csv": datadogCSVBody}, "renewal", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	assertShows(t, out, "infra_host,Infra Hosts")
}

func TestDatadogRenewalRefusesAFormatItDoesNotWrite(t *testing.T) {
	if _, err := runDatadog(t, nil, "renewal", "--format", "xml"); err == nil {
		t.Error("expected an error for --format xml")
	}
}

func TestDatadogRenewalCSVCannotBeNarrowed(t *testing.T) {
	if _, err := runDatadog(t, nil, "renewal", "infra_host", "--format", "csv"); err == nil {
		t.Error("expected an error: the spreadsheet is the whole table")
	}
}

func TestDatadogRenewalCSVFailures(t *testing.T) {
	if out, err := runDatadog(t, nil, "renewal", "--format", "csv"); err != nil || !strings.Contains(out, "no Datadog") {
		t.Errorf("a tenant without Datadog should be told so, got err=%v out=%s", err, out)
	}

	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	t.Cleanup(resetFlags)
	if _, _, err := executeCommand(t, "providers", "datadog", "renewal", "--format", "csv"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestDatadogRenewalCSVWhenTheAPICannotBeReached(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)

	if _, _, err := executeCommand(t, "providers", "datadog", "renewal", "--format", "csv"); err == nil {
		t.Error("expected an error when the API cannot be reached")
	}
}

func TestDatadogRenewalCSVPassesOnARefusalFromTheAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "not allowed")
	}))
	t.Cleanup(srv.Close)
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)

	_, _, err := executeCommand(t, "providers", "datadog", "renewal", "--format", "csv")
	if err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("err = %v, want the API's own message", err)
	}
}

func TestDatadogPlanNamesWhyEachProductCounts(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal/plan": datadogPlanBody}, "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	assertShows(t, out, "$14400.00", "24 months", "Grew past its median", "Above the contract",
		"Another tool also covers it", "a_new_rule", "$12000.00", notMeasured)
}

func TestDatadogPlanWithNothingToCut(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/renewal/plan": `{"data":{"term_months":12,"points":[]}}`}, "plan")
	if err != nil {
		t.Fatalf("plan error: %v", err)
	}
	assertShows(t, out, "No product grew")
}

func TestDatadogContractShowsEachLineAndWhyOneHasNoProjection(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/contract": datadogContractBody}, "contract")
	if err != nil {
		t.Fatalf("contract error: %v", err)
	}
	assertShows(t, out, "ctr-1", "infra_host", "120", "2026-11-20", "logs: Too early in the month", notMeasured)
}

func TestDatadogContractWithNoneOnFile(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/contract": `{"data":{"month":"2026-11-01","contracts":[]}}`}, "contract")
	if err != nil {
		t.Fatalf("contract error: %v", err)
	}
	assertShows(t, out, "No Datadog contract")
}

func TestDatadogHygieneShowsTheScoreAndEachRule(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hygiene": datadogHygieneBody}, "hygiene")
	if err != nil {
		t.Fatalf("hygiene error: %v", err)
	}
	assertShows(t, out, "87 of 100", "Unused custom metrics", "Failing", "Passed", "$450.00", "aws")
}

func TestDatadogMonthsMarkTheOnesStillOpen(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/months": datadogMonthsBody}, "months")
	if err != nil {
		t.Fatalf("months error: %v", err)
	}
	assertShows(t, out, "2026-09-01", "estimated", "month_to_date", "$950.00", notMeasured)
}

func TestDatadogMonthsOfOneProduct(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/months": datadogMonthsBody}, "months", "infra_host")
	if err != nil {
		t.Fatalf("months infra_host error: %v", err)
	}
	assertShows(t, out, "Infra Hosts, in hosts", "45", "$950.00")
}

func TestDatadogMonthsRefusesAProductTheBillDoesNotHold(t *testing.T) {
	if _, err := runDatadog(t, map[string]string{"/months": datadogMonthsBody}, "months", "nope"); err == nil {
		t.Error("expected an error for a product the bill does not hold")
	}
}

func TestDatadogMonthsWithNothingBilled(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/months": `{"data":{"months":[],"products":[]}}`}, "months")
	if err != nil {
		t.Fatalf("months error: %v", err)
	}
	assertShows(t, out, "no month yet")
}

func TestDatadogAWSShowsEachMonthAndTheSeller(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/aws-reconciliation": datadogAWSBody}, "aws-reconciliation")
	if err != nil {
		t.Fatalf("aws-reconciliation error: %v", err)
	}
	assertShows(t, out, "direct", "none", "2026-10-01", "$100.00", "$10.00", notMeasured)

	resetFlags()
	body := strings.Replace(datadogAWSBody, `"seller":null`, `"seller":"Datadog Inc"`, 1)
	out, err = runDatadog(t, map[string]string{"/aws-reconciliation": body}, "aws-reconciliation")
	if err != nil {
		t.Fatalf("aws-reconciliation error: %v", err)
	}
	assertShows(t, out, "Datadog Inc")
}

func TestDatadogTenantWithoutAConnectionIsToldSo(t *testing.T) {
	out, err := runDatadog(t, nil, "hygiene")
	if err != nil {
		t.Fatalf("a tenant without Datadog should not fail: %v", err)
	}
	assertShows(t, out, "no Datadog connection")
}

func TestDatadogFormattingFlagsPrintTheWholeAnswer(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hygiene": datadogHygieneBody}, "hygiene", "--json")
	if err != nil {
		t.Fatalf("hygiene --json error: %v", err)
	}
	assertShows(t, out, "finding_count")
}

func TestDatadogReadsNeedACredential(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	t.Cleanup(resetFlags)
	for _, args := range [][]string{{"hygiene"}, {"hosts", "list"}, {"hosts", "view", "c:web-1"}} {
		if _, _, err := executeCommand(t, append([]string{"providers", "datadog"}, args...)...); err == nil {
			t.Errorf("%v: expected an error when not authenticated", args)
		}
	}
}

func TestDatadogWebOpensTheArea(t *testing.T) {
	original := openBrowser
	var opened []string
	openBrowser = func(url string) error { opened = append(opened, url); return nil }
	defer func() { openBrowser = original }()

	for _, args := range [][]string{{"hygiene", "--web"}, {"hosts", "list", "--web"}, {"hosts", "view", "c:web-1", "--web"}} {
		if _, err := runDatadog(t, nil, args...); err != nil {
			t.Fatalf("%v error: %v", args, err)
		}
		resetFlags()
	}
	if len(opened) != 3 {
		t.Fatalf("opened = %v, want one page per command", opened)
	}
	for _, url := range opened {
		if !strings.HasSuffix(url, datadogWebPath) {
			t.Errorf("opened %q, want the Datadog area", url)
		}
	}
}

func TestDatadogListShowsThePageAndWarnsWhenItIsPartial(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts": datadogHostsBody}, "hosts", "list")
	if err != nil {
		t.Fatalf("hosts list error: %v", err)
	}
	assertShows(t, out, "web-1", "aws", wordYes, "no", "$23.00", notMeasured, datadogNoValue, "Page 1 of 2", "--page 2")
}

func TestDatadogListReadsShares(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/log-quotas": datadogQuotasBody}, "log-quotas", "list")
	if err != nil {
		t.Fatalf("log-quotas list error: %v", err)
	}
	assertShows(t, out, "25.0%", "$12.50", notMeasured)
}

func TestDatadogListSendsWhatWasAskedFor(t *testing.T) {
	asked := datadogServer(t, map[string]string{"/hosts": datadogHostsBody})
	_, _, err := executeCommand(t, "providers", "datadog", "hosts", "list", "--search", "web", "--sort-by", "name",
		"--sort-order", "asc", "--cloud", "aws", "--cloud", "gcp", "--team", "core", "--page", "2", "--page-size", "10")
	if err != nil {
		t.Fatalf("hosts list error: %v", err)
	}
	if len(*asked) != 1 {
		t.Fatalf("asked = %v, want one request", *asked)
	}
	for _, want := range []string{"search=web", "sort=name", "order=asc", "cloud=aws", "cloud=gcp", "team=core", "page=2", "page_size=10"} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("query %q should carry %s", (*asked)[0], want)
		}
	}
}

func TestDatadogListDefaultsToTheSamePageSizeAsTheOtherLists(t *testing.T) {
	asked := datadogServer(t, map[string]string{"/hosts": datadogHostsBody})
	if _, _, err := executeCommand(t, "providers", "datadog", "hosts", "list"); err != nil {
		t.Fatalf("hosts list error: %v", err)
	}
	if !strings.Contains((*asked)[0], "page_size=20") || strings.Contains((*asked)[0], "sort=") {
		t.Errorf("query %q should ask for 20 rows and leave the sort to the API", (*asked)[0])
	}
}

func TestDatadogFilterFlagsAreKebabCaseAndSendTheAPIsName(t *testing.T) {
	asked := datadogServer(t, map[string]string{"/metrics": datadogQuotasBody})
	if _, _, err := executeCommand(t, "providers", "datadog", "metrics", "list", "--metric-type", "gauge", "--queried", "false"); err != nil {
		t.Fatalf("metrics list error: %v", err)
	}
	for _, want := range []string{"metric_type=gauge", "queried=false"} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("query %q should carry %s", (*asked)[0], want)
		}
	}
}

func TestDatadogListRefusesAFilterItDoesNotHave(t *testing.T) {
	_, err := runDatadog(t, map[string]string{"/hosts": datadogHostsBody}, "hosts", "list", "--colour", "red")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("err = %v, want the unknown flag named", err)
	}

	resetFlags()
	_, err = runDatadog(t, map[string]string{"/team-coverage": datadogNoPageSizeBody}, "team-coverage", "list", "--cloud", "aws")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Errorf("err = %v, a list with no filters takes none", err)
	}
}

func TestDatadogListEmptyPage(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts": datadogEmptyBody}, "hosts", "list")
	if err != nil {
		t.Fatalf("hosts list error: %v", err)
	}
	assertShows(t, out, "Nothing in hosts")

	resetFlags()
	out, err = runDatadog(t, map[string]string{"/aws-resources": datadogReasonBody}, "aws-resources", "list")
	if err != nil {
		t.Fatalf("aws-resources list error: %v", err)
	}
	assertShows(t, out, "No AWS bill reaches")
}

func TestDatadogListWithNoPageSizeLeavesTheFooterOut(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/team-coverage": datadogNoPageSizeBody}, "team-coverage", "list")
	if err != nil {
		t.Fatalf("team-coverage list error: %v", err)
	}
	if strings.Contains(out, "Page ") {
		t.Errorf("no page size means no footer:\n%s", out)
	}
}

func TestDatadogListFormattingAndNoConnection(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts": datadogHostsBody}, "hosts", "list", "--json")
	if err != nil {
		t.Fatalf("hosts list --json error: %v", err)
	}
	assertShows(t, out, "c:web-1")

	resetFlags()
	out, err = runDatadog(t, nil, "hosts", "list")
	if err != nil {
		t.Fatalf("a tenant without Datadog should not fail: %v", err)
	}
	assertShows(t, out, "no Datadog connection")
}

func TestDatadogGrowthIsAlreadyAPercentage(t *testing.T) {
	body := `{"data":{"total":1,"page":1,"page_size":50,"partial":false,"facets":{},"items":[` +
		`{"name":"m","growth_pct":12.5},{"name":"new","growth_pct":null}]}}`
	out, err := runDatadog(t, map[string]string{"/metric-cardinality": body}, "metric-cardinality", "list")
	if err != nil {
		t.Fatalf("metric-cardinality list error: %v", err)
	}
	assertShows(t, out, "12.5%", notMeasured)
}

func TestDatadogViewShowsTheItemItsSavingsAndItsChanges(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts/c:web-1": datadogHostBody}, "hosts", "view", "c:web-1")
	if err != nil {
		t.Fatalf("hosts view error: %v", err)
	}
	assertShows(t, out, "web-1", "aws", "$23.00", "Open savings", "DD-1", "$40.00", "Changes", "someone@example.com", "--json")
}

func TestDatadogViewLeavesOutWhatTheItemDoesNotHave(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts/c:web-2": datadogBareHostBody}, "hosts", "view", "c:web-2")
	if err != nil {
		t.Fatalf("hosts view error: %v", err)
	}
	if strings.Contains(out, "Open savings") || strings.Contains(out, "Changes") {
		t.Errorf("an item with no savings or changes should not print their headers:\n%s", out)
	}
}

func TestDatadogViewWithFormattingFlags(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts/c:web-1": datadogHostBody}, "hosts", "view", "c:web-1", "--jq", ".data.name")
	if err != nil {
		t.Fatalf("hosts view --jq error: %v", err)
	}
	assertShows(t, out, "web-1")
}

func TestDatadogViewWithoutAConnection(t *testing.T) {
	out, err := runDatadog(t, nil, "hosts", "view", "x")
	if err != nil {
		t.Fatalf("a tenant without Datadog should not fail: %v", err)
	}
	assertShows(t, out, "no Datadog connection")
}

func TestDatadogViewExistsOnlyWhereTheAPIHasADetail(t *testing.T) {
	for _, kind := range datadogKinds {
		found, _, err := rootCmd.Find([]string{"providers", "datadog", kind.name, "view"})
		hasView := err == nil && found.Name() == "view"
		if hasView != (kind.view != nil) {
			t.Errorf("%s: view command = %v, want %v", kind.name, hasView, kind.view != nil)
		}
	}
}

func TestDatadogPassesOnARefusalFromTheAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":{"message":"sort must be one of name"}}`)
	}))
	t.Cleanup(srv.Close)
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)

	_, _, err := executeCommand(t, "providers", "datadog", "hosts", "list", "--sort-by", "colour")
	if err == nil || !strings.Contains(err.Error(), "sort must be one of name") {
		t.Errorf("err = %v, want the API's own message", err)
	}
}

func TestDatadogStaysOutOfTheGlobalHelp(t *testing.T) {
	kr.MockInit()
	t.Cleanup(resetFlags)

	global, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	assertShows(t, global.String(), "providers")
	if strings.Contains(global.String(), "datadog") {
		t.Errorf("a provider's commands belong under providers, not in the global help:\n%s", global)
	}

	providers, _, err := executeCommand(t, "providers", "--help")
	if err != nil {
		t.Fatalf("providers --help error: %v", err)
	}
	assertShows(t, providers.String(), "datadog", "list", "view")
}

// A table reads rows as maps, so the compiler cannot catch a column that names a
// field the SDK does not return: it would print a dash for every row.
func TestDatadogTablesNameFieldsTheSDKReturns(t *testing.T) {
	bodies := map[string]string{}
	for _, kind := range datadogKinds {
		bodies["/"+kind.name] = `{"data":{}}`
		bodies["/"+kind.name+"/x"] = `{"data":{}}`
	}
	datadogServer(t, bodies)
	client, err := newSDKClientFn()
	if err != nil {
		t.Fatalf("client error: %v", err)
	}
	ctx := context.Background()
	dd := client.SDK().Datadog

	for _, kind := range datadogKinds {
		list, err := kind.list(ctx, dd, newDatadogQuery(kind.listCommand()))
		if err != nil {
			t.Fatalf("%s list error: %v", kind.name, err)
		}
		assertColumnsExist(t, kind.name+" list", kind.columns, sdkRowFields(sdkDataType(list), "Items"))
		if kind.view == nil {
			continue
		}
		item, err := kind.view(ctx, dd, "x")
		if err != nil {
			t.Fatalf("%s view error: %v", kind.name, err)
		}
		itemType := sdkDataType(item)
		assertColumnsExist(t, kind.name+" view", kind.columns, sdkFields(itemType))
		assertColumnsExist(t, kind.name+" savings", datadogSavingColumns, sdkRowFields(itemType, "Recommendations"))
		detail, _ := itemType.FieldByName("Detail")
		assertColumnsExist(t, kind.name+" changes", datadogChangeColumns, sdkRowFields(detail.Type.Elem(), "Changes"))
	}
}

func sdkDataType(resp any) reflect.Type {
	data, _ := reflect.TypeOf(resp).Elem().FieldByName("Data")
	return data.Type.Elem()
}

func sdkRowFields(parent reflect.Type, listField string) map[string]bool {
	list, _ := parent.FieldByName(listField)
	return sdkFields(list.Type.Elem().Elem())
}

func sdkFields(row reflect.Type) map[string]bool {
	fields := map[string]bool{}
	for index := range row.NumField() {
		name, _, _ := strings.Cut(row.Field(index).Tag.Get("json"), ",")
		fields[name] = true
	}
	return fields
}

func assertColumnsExist(t *testing.T, table string, columns []datadogColumn, fields map[string]bool) {
	t.Helper()
	for _, column := range columns {
		if !fields[column.field] {
			t.Errorf("%s: column %q reads %q, which the SDK does not return", table, column.title, column.field)
		}
	}
}

func TestDatadogJSONKeepsTheFacets(t *testing.T) {
	out, err := runDatadog(t, map[string]string{"/hosts": datadogHostsBody}, "hosts", "list", "--jq", ".data.facets.cloud[0].value")
	if err != nil {
		t.Fatalf("hosts list --jq error: %v", err)
	}
	assertShows(t, out, "aws")
}

func TestDatadogBoolAndNumberFiltersSendWhatTheAPITakes(t *testing.T) {
	asked := datadogServer(t, map[string]string{"/log-indexes": datadogEmptyBody})
	if _, _, err := executeCommand(t, "providers", "datadog", "log-indexes", "list", "--retention-days", "15", "--retention-days", "30"); err != nil {
		t.Fatalf("log-indexes list error: %v", err)
	}
	for _, want := range []string{"retention_days=15", "retention_days=30"} {
		if !strings.Contains((*asked)[0], want) {
			t.Errorf("query %q should carry %s", (*asked)[0], want)
		}
	}
	resetFlags()

	_, err := runDatadog(t, nil, "metrics", "list", "--queried", "maybe")
	if err == nil || !strings.Contains(err.Error(), "--queried") {
		t.Errorf("err = %v, a flag that takes true or false should say so", err)
	}
}

// Anyone who can send a metric or register a host in a Datadog account picks its
// name, so a name must not reach the terminal able to move the cursor or plant a link.
const datadogEscape = "\\u001b[2J\\u001b]8;;https://example.com\\u0007"

func assertNoTerminalControl(t *testing.T, out string) {
	t.Helper()
	if strings.ContainsAny(out, "\x1b\x07") {
		t.Errorf("output should carry no terminal control characters:\n%q", out)
	}
	if !strings.Contains(out, "�") {
		t.Errorf("output should mark what it removed:\n%q", out)
	}
}

func TestDatadogNamesCannotReachTheTerminalRaw(t *testing.T) {
	escaped := func(body, name string) string {
		return strings.ReplaceAll(body, name, datadogEscape+name)
	}
	for _, test := range []struct {
		name, route, body string
		args              []string
	}{
		{"list", "/hosts", escaped(datadogHostsBody, "web-1"), []string{"hosts", "list"}},
		{"view", "/hosts/c:web-1", escaped(datadogHostBody, "web-1"), []string{"hosts", "view", "c:web-1"}},
		{"view changes", "/hosts/c:web-1", escaped(datadogHostBody, "someone@example.com"), []string{"hosts", "view", "c:web-1"}},
		{"renewal", "/renewal", escaped(datadogRenewalBody, "Infra Hosts"), []string{"renewal"}},
		{"renewal product", "/renewal", escaped(datadogRenewalBody, "Infra Hosts"), []string{"renewal", "infra_host"}},
		{"plan", "/renewal/plan", escaped(datadogPlanBody, "Infra Hosts"), []string{"plan"}},
		{"contract", "/contract", escaped(datadogContractBody, "ctr-1"), []string{"contract"}},
		{"contract line", "/contract", escaped(datadogContractBody, "infra_host"), []string{"contract"}},
		{"hygiene", "/hygiene", escaped(datadogHygieneBody, "Idle hosts"), []string{"hygiene"}},
		{"months", "/months", escaped(datadogMonthsBody, "Infra Hosts"), []string{"months"}},
		{"months product", "/months", escaped(datadogMonthsBody, "Infra Hosts"), []string{"months", "infra_host"}},
		{"month state", "/months", escaped(datadogMonthsBody, "estimated"), []string{"months"}},
		{"seller", "/aws-reconciliation", strings.Replace(datadogAWSBody, `"seller":null`, `"seller":"`+datadogEscape+`Datadog"`, 1), []string{"aws-reconciliation"}},
		{"invoiced", "/aws-reconciliation", escaped(datadogAWSBody, "direct"), []string{"aws-reconciliation"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Cleanup(resetFlags)
			out, err := runDatadog(t, map[string]string{test.route: test.body}, test.args...)
			if err != nil {
				t.Fatalf("%v error: %v", test.args, err)
			}
			assertNoTerminalControl(t, out)
		})
	}
}

func TestDatadogRenewalCSVIsCleanOnATerminalAndExactInAPipe(t *testing.T) {
	body := "product,label\r\ninfra_host,\x1b[2JInfra Hosts\r\n"

	piped, err := runDatadog(t, map[string]string{"/renewal.csv": body}, "renewal", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	if piped != body {
		t.Errorf("a pipe should get the API's bytes, got %q", piped)
	}
	resetFlags()

	original := output.IsTTY
	output.IsTTY = func() bool { return true }
	t.Cleanup(func() { output.IsTTY = original })
	shown, err := runDatadog(t, map[string]string{"/renewal.csv": body}, "renewal", "--format", "csv")
	if err != nil {
		t.Fatalf("renewal --format csv error: %v", err)
	}
	assertNoTerminalControl(t, shown)
	if strings.Count(shown, "\n") != 2 || strings.Contains(shown, "\r") {
		t.Errorf("a terminal should keep the rows on their own lines, got %q", shown)
	}
}

func TestProvidersViewCleansWhatTheBillNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"provider_name":"`+datadogEscape+`Datadog","top_services":[{"service":"`+datadogEscape+`logs","cost":1}]}}`)
	}))
	t.Cleanup(srv.Close)
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)

	out, _, err := executeCommand(t, "providers", "view", "datadog")
	if err != nil {
		t.Fatalf("providers view error: %v", err)
	}
	assertNoTerminalControl(t, out.String())
}
