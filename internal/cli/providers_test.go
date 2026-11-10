package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestProvidersListUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "providers", "list")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestProvidersListAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "providers", "list")
	if err == nil {
		t.Error("expected error when API fails")
	}
}

func TestProvidersList404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "providers", "list")
	if err != nil {
		t.Fatalf("expected no error for 404, got %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "not yet available") {
		t.Errorf("expected friendly 404 message, got %q", got)
	}
}

func TestProvidersList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": []interface{}{
				map[string]interface{}{
					"provider_id":   "aws-001",
					"provider_name": "AWS",
				},
			},
		})
	}))
	defer srv.Close()

	t.Run("table output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "providers", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "aws-001") || !strings.Contains(got, "AWS") {
			t.Errorf("output missing provider data: %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "providers", "list", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "data") {
			t.Errorf("JSON output missing data: %q", outBuf.String())
		}
	})

	t.Run("empty results", func(t *testing.T) {
		emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{},
			})
		}))
		defer emptySrv.Close()

		flagAPI = emptySrv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "providers", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "No providers connected") {
			t.Errorf("expected empty message, got %q", outBuf.String())
		}
	})
}

func TestIntegrationsStaysAnAliasOfProviders(t *testing.T) {
	alias, _, err := rootCmd.Find([]string{"integrations", "list"})
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}
	if alias != providersListCmd {
		t.Errorf("integrations list resolved to %q, want providers list", alias.CommandPath())
	}
}

const providerSummaryBody = `{"success":true,"data":{"provider_id":"datadog","provider_name":"Datadog",` +
	`"monthly_spending":1250,"forecasted_monthly_costs":1300,"potential_savings":45,"potential_savings_percentage":3.6,` +
	`"top_services":[{"service":"infra_host","cost":800,"change_percentage":2.5}]}}`

func providerSummaryServer(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/providers/datadog/costs/summary" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(providerSummaryBody))
	}))
	t.Cleanup(srv.Close)
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)
}

func TestProvidersViewShowsTheProviderItsSpendAndItsTopServices(t *testing.T) {
	providerSummaryServer(t)

	outBuf, _, err := executeCommand(t, "providers", "view", "datadog")
	if err != nil {
		t.Fatalf("view error: %v", err)
	}
	for _, want := range []string{"Datadog", "$1250.00", "$1300.00", "$45.00", "infra_host", "$800.00"} {
		if !strings.Contains(outBuf.String(), want) {
			t.Errorf("view should show %q:\n%s", want, outBuf)
		}
	}
}

func TestProvidersViewWithFormattingFlags(t *testing.T) {
	providerSummaryServer(t)

	outBuf, _, err := executeCommand(t, "providers", "view", "datadog", "--jq", ".data.monthly_spending")
	if err != nil {
		t.Fatalf("view --jq error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "1250") {
		t.Errorf("--jq should read under .data, the path every view uses: %s", outBuf)
	}
}

func TestProvidersViewRefusesAProviderThatIsNotConnected(t *testing.T) {
	providerSummaryServer(t)

	_, _, err := executeCommand(t, "providers", "view", "nope")
	if err == nil || !strings.Contains(err.Error(), "not a connected provider") {
		t.Errorf("err = %v, want it to say the provider is not connected", err)
	}
}

func TestProvidersViewWhenTheAPIFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	t.Cleanup(resetFlags)

	_, _, err := executeCommand(t, "providers", "view", "datadog")
	if err == nil || strings.Contains(err.Error(), "not a connected provider") {
		t.Errorf("err = %v, want the failure itself", err)
	}
}

func TestProvidersViewNeedsACredential(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	t.Cleanup(resetFlags)

	if _, _, err := executeCommand(t, "providers", "view", "aws"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}

func TestProvidersViewOpensTheProviderPage(t *testing.T) {
	original := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = original }()
	t.Cleanup(resetFlags)

	if _, _, err := executeCommand(t, "providers", "view", "datadog", "--web"); err != nil {
		t.Fatalf("view --web error: %v", err)
	}
	if !strings.HasSuffix(opened, "/providers/datadog") {
		t.Errorf("opened %q, want the provider's page", opened)
	}
}
