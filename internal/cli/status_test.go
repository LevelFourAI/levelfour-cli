package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestStatusUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "status")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestStatusJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "healthy"})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "status", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "healthy") {
		t.Errorf("JSON output missing status: %q", outBuf.String())
	}
}

func TestStatus(t *testing.T) {
	t.Run("healthy", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status": "healthy",
			})
		}))
		defer srv.Close()

		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "status")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "healthy") {
			t.Errorf("output missing 'healthy': %q", got)
		}
		if !strings.Contains(got, "Base URL") {
			t.Errorf("output missing 'Base URL': %q", got)
		}
	})

	t.Run("unreachable", func(t *testing.T) {
		flagAPI = "http://localhost:1"
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		_, errBuf, err := executeCommand(t, "status")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := errBuf.String()
		if !strings.Contains(got, "API unreachable") {
			t.Errorf("expected unreachable error, got %q", got)
		}
	})
}
