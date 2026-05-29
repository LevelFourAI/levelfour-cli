package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestIntegrationsListUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "integrations", "list")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestIntegrationsListAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "integrations", "list")
	if err == nil {
		t.Error("expected error when API fails")
	}
}

func TestIntegrationsList404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "integrations", "list")
	if err != nil {
		t.Fatalf("expected no error for 404, got %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "not yet available") {
		t.Errorf("expected friendly 404 message, got %q", got)
	}
}

func TestIntegrationsList(t *testing.T) {
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

		outBuf, _, err := executeCommand(t, "integrations", "list")
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

		outBuf, _, err := executeCommand(t, "integrations", "list", "--json")
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

		outBuf, _, err := executeCommand(t, "integrations", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "No providers connected") {
			t.Errorf("expected empty message, got %q", outBuf.String())
		}
	})
}
