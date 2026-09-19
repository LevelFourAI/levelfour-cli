package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	kr "github.com/zalando/go-keyring"
)

const (
	overviewBody = `{"data":{"coverage_pct":45.7,"total_committed_monthly":1000,` +
		`"estimated_waste_monthly":50,"commitment_count":12,"expiring_count":3,"accounts":[]}}`

	byServiceBody = `{"data":{"provider":"aws","services":[{"service":"ec2","service_label":"EC2",` +
		`"ri":{"utilization_pct":98.2,"coverage_pct":60.1,"unused_monthly":12.5,"commitment_count":4},` +
		`"sp":{"utilization_pct":100,"coverage_pct":0,"unused_monthly":0,"commitment_count":1}}]}}`

	gcpByServiceBody = `{"data":{"provider":"gcp","services":[{"service":"compute","service_label":"Compute",` +
		`"ri":{"utilization_pct":91.4,"coverage_pct":52.0,"unused_monthly":8,"commitment_count":3},` +
		`"sp":{"utilization_pct":0,"coverage_pct":0,"unused_monthly":0,"commitment_count":0}}]}}`

	esrBody = `{"data":{"period":"2026-08","scope":"eligible","measured_share_pct":82.4,` +
		`"totals":{"measured":true,"total_rate_pct":31.2,"commitment_rate_pct":18.9,` +
		`"negotiated_rate_pct":12.3},"accounts":[]}}`
)

// commitmentsServer answers the commitments routes named in bodies and 404s the
// rest, which is also how a deployment without the surface behaves. A key ending
// in "/" matches every path beneath it, for the two routes that carry an id.
func commitmentsServer(t *testing.T, providers []string, bodies map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			writeProviderList(w, providers)
			return
		}
		if !strings.HasPrefix(r.URL.Path, api.CommitmentsPath) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		route := strings.TrimPrefix(r.URL.Path, api.CommitmentsPath)
		if body, ok := bodies[route]; ok {
			_, _ = io.WriteString(w, body)
			return
		}
		for prefix, body := range bodies {
			if strings.HasSuffix(prefix, "/") && strings.HasPrefix(route, prefix) {
				_, _ = io.WriteString(w, body)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeProviderList(w http.ResponseWriter, providers []string) {
	data := make([]any, 0, len(providers))
	for _, id := range providers {
		data = append(data, map[string]any{"provider_id": id, "provider_name": strings.ToUpper(id)})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func useCommitmentsServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	kr.MockInit()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)
}

func awsSummaryServer(t *testing.T) *httptest.Server {
	return commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/overview":       overviewBody,
		"/by-service":     byServiceBody,
		"/esr":            esrBody,
		"/coverage-rates": coverageBody,
	})
}

func TestCommitmentsSummaryUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "summary"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestCommitmentsSummaryReportsTheRateAndTheSplit(t *testing.T) {
	useCommitmentsServer(t, awsSummaryServer(t))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	for _, want := range []string{"31.2%", "18.9%", "12.3%", "EC2", "RI Util", "SP Util"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	// The spend-weighted figure, not the overview's own 45.7%, which averages
	// percentages across commitments and would disagree with `commitments coverage`.
	if !strings.Contains(out.String(), "80.8%") || strings.Contains(out.String(), "45.7%") {
		t.Errorf("coverage should be the weighted figure:\n%s", out.String())
	}
}

func TestCommitmentsSummaryNamesAPartialMeasurement(t *testing.T) {
	useCommitmentsServer(t, awsSummaryServer(t))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	if !strings.Contains(out.String(), "82.4% of eligible spend") {
		t.Errorf("output should say how much was measured:\n%s", out.String())
	}
}

// Google Cloud has no on-demand equivalent to divide by and no lifecycle export,
// so the rate and the expiring count must read as unmeasured rather than as zero.
func TestCommitmentsSummaryRefusesToInventAGoogleCloudRate(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerGCP}, map[string]string{
		"/overview":       overviewBody,
		"/by-service":     gcpByServiceBody,
		"/coverage-rates": gcpCoverageBody,
	}))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	if strings.Count(out.String(), notMeasured) < 2 {
		t.Errorf("rate and expiring count should both read as unmeasured:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "CUD Util") {
		t.Errorf("Google Cloud instruments should not be labelled RI:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Spend Util") {
		t.Errorf("an instrument holding nothing should not get columns:\n%s", out.String())
	}
}

// The coverage read is newer than the rest of summary, so an API serving the
// others without it still produces a summary rather than nothing.
func TestCommitmentsSummarySurvivesAMissingCoverageRoute(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/overview":   overviewBody,
		"/by-service": byServiceBody,
		"/esr":        esrBody,
	}))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	if !strings.Contains(out.String(), "EC2") {
		t.Errorf("the rest of the summary should still render:\n%s", out.String())
	}
	if !strings.Contains(out.String(), notMeasured) {
		t.Errorf("coverage should read as unmeasured:\n%s", out.String())
	}
}

// Any failure that is not a missing route is a real one.
func TestCommitmentsSummarySurfacesACoverageFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			writeProviderList(w, []string{providerAWS})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/coverage-rates") {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, overviewBody)
	}))
	defer srv.Close()
	useCommitmentsServer(t, srv)

	if _, _, err := executeCommand(t, "commitments", "summary"); err == nil {
		t.Error("a 500 on the coverage read should surface, not degrade silently")
	}
}

func TestCommitmentsSummaryJSONCarriesEveryRead(t *testing.T) {
	useCommitmentsServer(t, awsSummaryServer(t))
	flagJSON = true

	out, _, err := executeCommand(t, "commitments", "summary", "--json")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	for _, key := range []string{"overview", "by_service", "esr"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("JSON output missing %q: %v", key, payload)
		}
	}
}

func TestCommitmentsSummaryReportsAMissingSurface(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("a missing surface is not an error: %v", err)
	}
	if !strings.Contains(out.String(), "not yet available") {
		t.Errorf("output should say the surface is not available:\n%s", out.String())
	}
}

func TestCommitmentsSummaryWithNoCommitments(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, map[string]string{
		"/overview":       overviewBody,
		"/by-service":     `{"data":{"provider":"aws","services":[]}}`,
		"/esr":            `{"data":{"scope":"eligible","measured_share_pct":100,"totals":null,"accounts":[]}}`,
		"/coverage-rates": unmeasuredCoverageBody,
	}))

	out, _, err := executeCommand(t, "commitments", "summary")
	if err != nil {
		t.Fatalf("summary error: %v", err)
	}
	if !strings.Contains(out.String(), "No commitments found") {
		t.Errorf("output should say there are none:\n%s", out.String())
	}
	if !strings.Contains(out.String(), notMeasured) {
		t.Errorf("an unmeasured rate should say so:\n%s", out.String())
	}
}

func TestCommitmentsSummaryOpensTheWeb(t *testing.T) {
	useCommitmentsServer(t, awsSummaryServer(t))
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()

	if _, _, err := executeCommand(t, "commitments", "summary", "--web"); err != nil {
		t.Fatalf("summary --web error: %v", err)
	}
	if !strings.Contains(opened, "/commitments") {
		t.Errorf("opened %q, want the commitments page", opened)
	}
}

func TestResolveCommitmentProviderPrefersOneItCanAnswerFor(t *testing.T) {
	srv := commitmentsServer(t, []string{"azure", providerGCP, providerAWS}, nil)
	useCommitmentsServer(t, srv)

	client, err := newSDKClient()
	if err != nil {
		t.Fatalf("newSDKClient() error: %v", err)
	}
	provider, err := resolveCommitmentProvider(t.Context(), client, "")
	if err != nil {
		t.Fatalf("resolveCommitmentProvider() error: %v", err)
	}
	if provider != providerGCP {
		t.Errorf("provider = %q, want the first supported one", provider)
	}
}

func TestResolveCommitmentProviderFallsBackToTheFirstConnected(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{"azure"}, nil))

	client, err := newSDKClient()
	if err != nil {
		t.Fatalf("newSDKClient() error: %v", err)
	}
	provider, err := resolveCommitmentProvider(t.Context(), client, "")
	if err != nil {
		t.Fatalf("resolveCommitmentProvider() error: %v", err)
	}
	if provider != "azure" {
		t.Errorf("provider = %q, want azure", provider)
	}
}

func TestResolveCommitmentProviderHonoursTheOverride(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, []string{providerAWS}, nil))

	client, err := newSDKClient()
	if err != nil {
		t.Fatalf("newSDKClient() error: %v", err)
	}
	provider, err := resolveCommitmentProvider(t.Context(), client, providerGCP)
	if err != nil {
		t.Fatalf("resolveCommitmentProvider() error: %v", err)
	}
	if provider != providerGCP {
		t.Errorf("provider = %q, want the override", provider)
	}
}

func TestResolveCommitmentProviderWithNothingConnected(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, nil, nil))

	client, err := newSDKClient()
	if err != nil {
		t.Fatalf("newSDKClient() error: %v", err)
	}
	if _, err := resolveCommitmentProvider(t.Context(), client, ""); err == nil {
		t.Error("expected an error when no provider is connected")
	}
}

func TestResolveCommitmentProviderSurfacesAListFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	useCommitmentsServer(t, srv)

	client, err := newSDKClient()
	if err != nil {
		t.Fatalf("newSDKClient() error: %v", err)
	}
	if _, err := resolveCommitmentProvider(t.Context(), client, ""); err == nil {
		t.Error("expected the provider listing error to surface")
	}
}

// Every commitments command resolves a provider first, so a failure there has
// to surface rather than leaving the command to query a provider it never got.
func TestCommitmentsCommandSurfacesAProviderResolutionFailure(t *testing.T) {
	useCommitmentsServer(t, commitmentsServer(t, nil, map[string]string{"/overview": overviewBody}))

	_, _, err := executeCommand(t, "commitments", "summary")
	if err == nil || !strings.Contains(err.Error(), "no providers connected") {
		t.Errorf("err = %v, want the resolution failure", err)
	}
}

func TestProviderLabelFallsBackToTheID(t *testing.T) {
	if got := providerLabel(providerGCP); got != "Google Cloud" {
		t.Errorf("providerLabel(gcp) = %q", got)
	}
	if got := providerLabel("oracle"); got != "oracle" {
		t.Errorf("providerLabel(oracle) = %q, want the id back", got)
	}
}

func TestInstrumentNamesFallBackToTheAWSPair(t *testing.T) {
	if got := instrumentNames("oracle"); got != [2]string{"RI", "SP"} {
		t.Errorf("instrumentNames(oracle) = %v", got)
	}
}

func TestUnavailableForProviderLetsAWSThrough(t *testing.T) {
	captureOutput(t)
	if unavailableForProvider(providerAWS, "should not print for %s") {
		t.Error("AWS should never be refused")
	}
	if !unavailableForProvider(providerGCP, "not measured for %s") {
		t.Error("a non-AWS provider should be refused")
	}
}

func TestFirstErrorReturnsNilWhenEverythingSucceeded(t *testing.T) {
	if err := firstError(nil, nil); err != nil {
		t.Errorf("firstError() = %v, want nil", err)
	}
}

func TestEffectiveRateNeverGuesses(t *testing.T) {
	measured := 12.5
	cases := []struct {
		name string
		esr  *api.CommitmentEsr
		want string
	}{
		{"absent", nil, notMeasured},
		{"no totals", &api.CommitmentEsr{}, notMeasured},
		{"unmeasured", &api.CommitmentEsr{Totals: &api.EsrRates{Measured: false}}, notMeasured},
		{"measured", &api.CommitmentEsr{Totals: &api.EsrRates{Measured: true, TotalRatePct: &measured}}, "12.5%"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveRate(tc.esr); got != tc.want {
				t.Errorf("effectiveRate() = %q, want %q", got, tc.want)
			}
		})
	}
}
