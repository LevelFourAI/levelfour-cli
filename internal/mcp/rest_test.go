package mcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

var errAPI = errors.New("API error (401): Unauthorized")

func restClient(t *testing.T, handler http.HandlerFunc) Fetcher {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := api.NewClient(srv.URL, "l4_test_testkey123456789a", "test")
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return NewRESTFetcher(client)
}

func TestRESTFetcherUnwrapsTheResponseEnvelope(t *testing.T) {
	f := restClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("credential was not sent: %q", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": true,
			"data":    map[string]any{"total_spend": 1234.5},
		})
	})

	got, err := f.Fetch("/api/v1/costs/summary")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.(map[string]any)["total_spend"] != 1234.5 {
		t.Errorf("fetch returned %v", got)
	}
}

func TestRESTFetcherPassesThroughAnUnwrappedBody(t *testing.T) {
	f := restClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "healthy"})
	})

	got, err := f.Fetch("/health")
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.(map[string]any)["status"] != "healthy" {
		t.Errorf("fetch returned %v", got)
	}
}

func TestRESTFetcherSurfacesAPIErrors(t *testing.T) {
	f := restClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "read scope required"},
		})
	})

	_, err := f.Fetch("/api/v1/costs/summary")
	if err == nil || !strings.Contains(err.Error(), "read scope required") {
		t.Fatalf("err = %v, want the API message", err)
	}
}
