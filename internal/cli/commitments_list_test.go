package cli

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

const (
	// 18 days and 94 days, so a 30 day window holds the first and not the second.
	portfolioBody = `{"data":{"basis":"net","totals":{"commitment_count":2,"fee_monthly_list":900,` +
		`"fee_monthly_net":800,"protects_monthly":12400,"rightsizing_monthly":300,` +
		`"expiring_within_30d":1,"organization_count":1},"rows":[` +
		`{"id":"ri-a1b2","service":"ec2","service_label":"EC2","kind":"reserved_instance",` +
		`"holder_account_id":"111122223333","holder_account_name":"prod","expires_in_seconds":1555200,` +
		`"end_at":"2026-10-07T00:00:00Z","utilization_pct":98.2,"coverage_pct":61.4,` +
		`"protects_monthly":12400,"saturated":false,"status":"active"},` +
		`{"id":"sp-9f3e","service":"compute","service_label":"Compute","kind":"savings_plan",` +
		`"holder_account_id":"111122223333","holder_account_name":"payer","expires_in_seconds":8121600,` +
		`"utilization_pct":100,"coverage_pct":0,"protects_monthly":null,"saturated":true,"status":"active"}]}}`

	// A Committed Use Discount now carries a fee and an expiry through this route.
	gcpPortfolioBody = `{"data":{"basis":"net","totals":{"commitment_count":1,` +
		`"fee_monthly_list":1200,"fee_monthly_net":1200,"protects_monthly":1200,` +
		`"rightsizing_monthly":0,"expiring_within_30d":0,"organization_count":1},"rows":[` +
		`{"id":"cud-1","service":"compute","service_label":"Compute",` +
		`"kind":"committed_use_discount","holder_account_id":"proj-1",` +
		`"holder_account_name":"Project One","expires_in_seconds":7776000,` +
		`"end_at":"2026-12-18T00:00:00Z","utilization_pct":91.4,"coverage_pct":52,` +
		`"protects_monthly":1200,"saturated":false,"status":"active"}]}}`
)

func awsPortfolioServer(t *testing.T) {
	t.Helper()
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/portfolio": portfolioBody,
	}))
}

func TestCommitmentsListShowsTheLedgerSoonestFirst(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	for _, want := range []string{"ri-a1b2", "sp-9f3e", "18d", "94d", "prod", "$800.00/mo"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

// Nothing measures what one Savings Plan covers, so its cell must not read 0.0%.
func TestCommitmentsListWillNotPrintASavingsPlanCoverage(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), notMeasured) {
		t.Errorf("a Savings Plan coverage should read as unmeasured:\n%s", out.String())
	}
	// Anchored, because a utilization of 100.0% ends in the same four characters.
	if regexp.MustCompile(`\b0\.0%`).MatchString(out.String()) {
		t.Errorf("an unmeasured coverage must never render as a zero percentage:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "saturated") {
		t.Errorf("a saturated commitment should be named:\n%s", out.String())
	}
}

func TestCommitmentsListNarrowsToAWindow(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "list", "--expiring-within", "30d")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), "ri-a1b2") || strings.Contains(out.String(), "sp-9f3e") {
		t.Errorf("only the commitment inside the window should be listed:\n%s", out.String())
	}
}

func TestCommitmentsListRejectsABadWindow(t *testing.T) {
	awsPortfolioServer(t)

	if _, _, err := executeCommand(t, "commitments", "list", "--expiring-within", "soon"); err == nil {
		t.Error("expected an error for an unparseable window")
	}
}

func TestCommitmentsListFiltersByKindAndStatus(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "list", "--kind", "sp")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if strings.Contains(out.String(), "ri-a1b2") {
		t.Errorf("--kind sp should drop the reservation:\n%s", out.String())
	}

	awsPortfolioServer(t)
	out, _, err = executeCommand(t, "commitments", "list", "--status", "expired")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), "No commitments match") {
		t.Errorf("a filter matching nothing should say so:\n%s", out.String())
	}
}

func TestCommitmentsListRefusesAKindFromAnotherProvider(t *testing.T) {
	awsPortfolioServer(t)

	_, _, err := executeCommand(t, "commitments", "list", "--kind", "cud")
	if err == nil || !strings.Contains(err.Error(), "not a kind AWS uses") {
		t.Errorf("err = %v, want a refusal naming the provider", err)
	}
}

func TestCommitmentsListRefusesAnUnknownKind(t *testing.T) {
	awsPortfolioServer(t)

	if _, _, err := executeCommand(t, "commitments", "list", "--kind", "coupon"); err == nil {
		t.Error("expected an error for an unknown kind")
	}
}

// The basis reaches the query, and the totals follow the basis the response
// reports rather than the one that was asked for.
func TestCommitmentsListReadsTheListBasis(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			writeProviderList(w, []string{providerAWS})
			return
		}
		if r.URL.Query().Get("basis") != "list" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, strings.Replace(portfolioBody, `"basis":"net"`, `"basis":"list"`, 1))
	}))
	defer srv.Close()
	useCommitmentsServer(t, srv)

	out, _, err := executeCommand(t, "commitments", "list", "--basis", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), "$900.00/mo") || !strings.Contains(out.String(), "Fee (list)") {
		t.Errorf("--basis list should total the list fee:\n%s", out.String())
	}
}

func TestCommitmentsListReadsGoogleCloudFromThePortfolio(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/portfolio": gcpPortfolioBody,
	}))

	out, _, err := executeCommand(t, "commitments", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	for _, want := range []string{"cud-1", "CUD", "Project One", "$1200.00/mo", "90d", "52.0%"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), notMeasured) {
		t.Errorf("nothing here is unmeasured any more:\n%s", out.String())
	}
}

func TestCommitmentsListFiltersGoogleCloudKinds(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{"/portfolio": gcpPortfolioBody}))

	out, _, err := executeCommand(t, "commitments", "list", "--kind", "cud")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), "cud-1") {
		t.Errorf("--kind cud should keep the row:\n%s", out.String())
	}

	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{"/portfolio": gcpPortfolioBody}))
	if _, _, err = executeCommand(t, "commitments", "list", "--kind", "ri"); err == nil {
		t.Error("--kind ri is not a Google Cloud kind and should be refused")
	}
}

// A flag that shapes the table but not the payload has to say so, or a script
// reading --json silently gets rows it asked to exclude.
func TestCommitmentsListSaysWhichFiltersTheJSONIgnores(t *testing.T) {
	awsPortfolioServer(t)

	_, errOut, err := executeCommand(t, "commitments", "list", "--json", "--kind", "ri", "--status", "active")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	notice := errOut.String()
	if !strings.Contains(notice, "--kind") || !strings.Contains(notice, "--status") {
		t.Errorf("the notice should name both flags:\n%s", notice)
	}
	if !strings.Contains(notice, "--jq") {
		t.Errorf("the notice should say how to narrow the payload:\n%s", notice)
	}
}

func TestCommitmentsListStaysQuietWhenNoFilterIsSet(t *testing.T) {
	awsPortfolioServer(t)

	_, errOut, err := executeCommand(t, "commitments", "list", "--json")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if strings.Contains(errOut.String(), "narrowed the table") {
		t.Errorf("no filter was set, so there is nothing to explain:\n%s", errOut.String())
	}
}

func TestCommitmentsListWithNoGoogleCloudRows(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/portfolio": `{"data":{"basis":"net","totals":{"commitment_count":0},"rows":[]}}`,
	}))

	out, _, err := executeCommand(t, "commitments", "list")
	if err != nil {
		t.Fatalf("list error: %v", err)
	}
	if !strings.Contains(out.String(), "No commitments match") {
		t.Errorf("output should say there are none:\n%s", out.String())
	}
}

func TestCommitmentsListJSON(t *testing.T) {
	awsPortfolioServer(t)
	if _, _, err := executeCommand(t, "commitments", "list", "--jq", ".data.rows[0].id"); err != nil {
		t.Fatalf("list --jq error: %v", err)
	}

	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{"/portfolio": gcpPortfolioBody}))
	out, _, err := executeCommand(t, "commitments", "list", "--json")
	if err != nil {
		t.Fatalf("list --json error: %v", err)
	}
	if !strings.Contains(out.String(), "cud-1") {
		t.Errorf("JSON output should carry the payload:\n%s", out.String())
	}
}

func TestCommitmentsListReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "list")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}

	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, nil))
	if _, _, err = executeCommand(t, "commitments", "list"); err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
}

func TestCommitmentsListOpensTheWeb(t *testing.T) {
	awsPortfolioServer(t)
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()

	if _, _, err := executeCommand(t, "commitments", "list", "--web"); err != nil {
		t.Fatalf("list --web error: %v", err)
	}
	if !strings.Contains(opened, "/commitments") {
		t.Errorf("opened %q, want the commitments page", opened)
	}
}

func TestCommitmentsListUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "list"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestCommitmentsExpiringListsTheDefaultWindow(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "expiring")
	if err != nil {
		t.Fatalf("expiring error: %v", err)
	}
	// 90 days holds the reservation at 18 days and not the plan at 94.
	if !strings.Contains(out.String(), "ri-a1b2") || strings.Contains(out.String(), "sp-9f3e") {
		t.Errorf("the default window should hold only the nearer commitment:\n%s", out.String())
	}
}

func TestCommitmentsExpiringNarrowsAndEmpties(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "expiring", "--within", "10d")
	if err != nil {
		t.Fatalf("expiring error: %v", err)
	}
	if !strings.Contains(out.String(), "No commitments lapse within 10d") {
		t.Errorf("an empty window should say so:\n%s", out.String())
	}
}

// The gate is the reason this command exists: a lapse inside the window has to
// stop a pipeline, not just print.
func TestCommitmentsExpiringFailsWhenSomethingLapsesInTheWindow(t *testing.T) {
	awsPortfolioServer(t)

	out, errOut, err := executeCommand(t, "commitments", "expiring", "--fail-within", "30d")
	if !errors.Is(err, ErrIssuesFound) {
		t.Fatalf("err = %v, want ErrIssuesFound", err)
	}
	if !strings.Contains(errOut.String(), "lapse within 30d") {
		t.Errorf("the failure should name the window:\n%s", errOut.String())
	}
	// The listing widens to the gate window so the output explains the exit code.
	if !strings.Contains(out.String(), "ri-a1b2") {
		t.Errorf("the breaching commitment should be listed:\n%s", out.String())
	}
}

func TestCommitmentsExpiringPassesWhenNothingLapses(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "expiring", "--fail-within", "5d")
	if err != nil {
		t.Fatalf("expiring error: %v", err)
	}
	if !strings.Contains(out.String(), "Nothing lapses within 5d") {
		t.Errorf("a clean run should say so:\n%s", out.String())
	}
}

func TestCommitmentsExpiringKeepsAnExplicitListingWindow(t *testing.T) {
	awsPortfolioServer(t)

	out, _, err := executeCommand(t, "commitments", "expiring", "--within", "180d", "--fail-within", "5d")
	if err != nil {
		t.Fatalf("expiring error: %v", err)
	}
	if !strings.Contains(out.String(), "sp-9f3e") {
		t.Errorf("an explicit --within should still list its own window:\n%s", out.String())
	}
}

func TestCommitmentsExpiringRejectsBadWindows(t *testing.T) {
	awsPortfolioServer(t)
	if _, _, err := executeCommand(t, "commitments", "expiring", "--within", "soon"); err == nil {
		t.Error("expected an error for an unparseable --within")
	}

	awsPortfolioServer(t)
	if _, _, err := executeCommand(t, "commitments", "expiring", "--fail-within", "soon"); err == nil {
		t.Error("expected an error for an unparseable --fail-within")
	}
}

// A gate that cannot measure must not fail a build for ever.
func TestCommitmentsExpiringExitsCleanWhereExpiryIsUnmeasured(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/portfolio": portfolioBody,
	}))

	out, _, err := executeCommand(t, "commitments", "expiring", "--fail-within", "30d")
	if err != nil {
		t.Fatalf("an unmeasurable gate must not fail: %v", err)
	}
	if !strings.Contains(out.String(), "not measured for Google Cloud") {
		t.Errorf("output should name why nothing was checked:\n%s", out.String())
	}
}

// Asking for JSON must not disarm the gate. A build that keeps passing because
// someone added --json is the failure this command exists to prevent.
func TestCommitmentsExpiringStillGatesUnderAFormattingFlag(t *testing.T) {
	awsPortfolioServer(t)

	out, errOut, err := executeCommand(t, "commitments", "expiring", "--json", "--fail-within", "30d")
	if !errors.Is(err, ErrIssuesFound) {
		t.Fatalf("err = %v, want ErrIssuesFound", err)
	}
	if !strings.Contains(errOut.String(), "lapse within 30d") {
		t.Errorf("the failure should go to stderr:\n%s", errOut.String())
	}
	if !strings.Contains(out.String(), `"basis"`) {
		t.Errorf("stdout should still carry the payload:\n%s", out.String())
	}
}

// --quiet is the documented "communicate through the exit code" mode, which is
// how a pipeline runs the gate without noise.
func TestCommitmentsExpiringGatesSilentlyUnderQuiet(t *testing.T) {
	awsPortfolioServer(t)

	out, errOut, err := executeCommand(t, "commitments", "expiring", "--quiet", "--fail-within", "30d")
	if !errors.Is(err, ErrIssuesFound) {
		t.Fatalf("err = %v, want ErrIssuesFound", err)
	}
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("--quiet should print nothing, got %q / %q", out.String(), errOut.String())
	}
}

func TestCommitmentsExpiringSurfacesAnOutputFailure(t *testing.T) {
	awsPortfolioServer(t)

	if _, _, err := executeCommand(t, "commitments", "expiring", "--jq", "|||"); err == nil {
		t.Error("expected the output failure to surface rather than fall through to the gate")
	}
}

func TestCommitmentsExpiringJSONAndWeb(t *testing.T) {
	awsPortfolioServer(t)
	if _, _, err := executeCommand(t, "commitments", "expiring", "--json"); err != nil {
		t.Fatalf("expiring --json error: %v", err)
	}

	awsPortfolioServer(t)
	original := openBrowser
	openBrowser = func(string) error { return nil }
	defer func() { openBrowser = original }()
	if _, _, err := executeCommand(t, "commitments", "expiring", "--web"); err != nil {
		t.Fatalf("expiring --web error: %v", err)
	}
}

func TestCommitmentsExpiringReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "expiring")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsExpiringUnauthenticated(t *testing.T) {
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "expiring"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

// A row with no measured expiry is dropped rather than counted, so a window
// never reports on a commitment it cannot date.
func TestWithinWindowDropsUndatedRows(t *testing.T) {
	soon := int64(3600)
	rows := []api.CommitmentPortfolioRow{{ID: "dated", ExpiresInSeconds: &soon}, {ID: "undated"}}

	kept := withinWindow(rows, 24*time.Hour)
	if len(kept) != 1 || kept[0].ID != "dated" {
		t.Errorf("kept = %+v, want only the dated row", kept)
	}
}

func TestKindLabelFallsBackToTheStoredValue(t *testing.T) {
	if got := kindLabel("committed_use_discount"); got != "CUD" {
		t.Errorf("kindLabel() = %q, want CUD", got)
	}
	if got := kindLabel("mystery"); got != "mystery" {
		t.Errorf("kindLabel() = %q, want the value back", got)
	}
}

func TestEndDateCellNamesAnAbsentTerm(t *testing.T) {
	if got := endDateCell(""); got != notMeasured {
		t.Errorf("endDateCell(\"\") = %q, want %q", got, notMeasured)
	}
	if got := endDateCell("2026-10-07"); got != "2026-10-07" {
		t.Errorf("endDateCell() = %q", got)
	}
}
