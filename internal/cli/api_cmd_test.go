package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func TestAPICmdUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "/api/v1/health")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error = %q, want 'not authenticated'", err.Error())
	}
}

func TestAPICmdGetSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/health" {
			t.Errorf("expected /api/v1/health, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "/api/v1/health", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "healthy") {
		t.Errorf("output missing status: %q", got)
	}
}

func TestAPICmdPostWithFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test" {
			t.Errorf("expected name=test, got %v", body["name"])
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"created": true,
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/resources", "-f", "name=test", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "created") {
		t.Errorf("output missing created: %q", got)
	}
}

func TestAPICmdIncludeHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-Id", "abc-123")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "/api/v1/health", "--include", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "HTTP") {
		t.Errorf("output missing HTTP status line: %q", got)
	}
	if !strings.Contains(got, "X-Request-Id") {
		t.Errorf("output missing header: %q", got)
	}
}

func TestAPICmdInvalidFieldFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/test", "-f", "invalid-no-equals")
	if err == nil {
		t.Error("expected error for invalid field format")
	}
	if !strings.Contains(err.Error(), "invalid field format") {
		t.Errorf("error = %q, want 'invalid field format'", err.Error())
	}
}

func TestAPICmdGetJSONWithoutFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "healthy",
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "/api/v1/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "healthy") {
		t.Errorf("output missing status without --json: %q", got)
	}
	if !strings.Contains(got, "{") {
		t.Errorf("expected JSON output even without --json flag: %q", got)
	}
}

func TestAPICmdGetNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("plain text response"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "/api/v1/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "plain text response") {
		t.Errorf("output missing raw response: %q", got)
	}
}

func TestAPICmdGetNonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("not json error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "/api/v1/health")
	if err == nil {
		t.Error("expected error for 500 non-JSON response")
	}
	if !strings.Contains(err.Error(), "API error (500)") {
		t.Errorf("error = %q, want 'API error (500)'", err.Error())
	}
}

func TestAPICmdGetJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "bad request"})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "/api/v1/health")
	if err == nil {
		t.Error("expected error for 400 JSON response")
	}
	if !strings.Contains(err.Error(), "API error (400)") {
		t.Errorf("error = %q, want 'API error (400)'", err.Error())
	}
}

func TestAPICmdPostIncludeHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Custom", "value")
		json.NewEncoder(w).Encode(map[string]interface{}{"result": "ok"})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/test", "-f", "key=value", "--include")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "HTTP") {
		t.Errorf("output missing HTTP status: %q", got)
	}
	if !strings.Contains(got, "X-Custom") {
		t.Errorf("output missing custom header: %q", got)
	}
}

func TestAPICmdPostNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("plain response"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/test", "-f", "k=v")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "plain response") {
		t.Errorf("output missing raw response: %q", got)
	}
}

func TestAPICmdPostNonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/test", "-f", "k=v")
	if err == nil {
		t.Error("expected error for 500 POST response")
	}
}

func TestAPICmdPostJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "validation"})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "-X", "POST", "/api/v1/test", "-f", "k=v")
	if err == nil {
		t.Error("expected error for 422 POST response")
	}
}

func TestAPICmdDeleteMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "DELETE", "/api/v1/resource/1", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "deleted") {
		t.Errorf("output missing response: %q", outBuf.String())
	}
}

func TestAPICmdDeleteInclude(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Trace", "trace-id")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "DELETE", "/api/v1/resource/1", "--include")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "HTTP") {
		t.Errorf("output missing HTTP status: %q", got)
	}
	if !strings.Contains(got, "X-Trace") {
		t.Errorf("output missing trace header: %q", got)
	}
}

func TestAPICmdDeleteNonJSONResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("deleted ok"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "api", "-X", "DELETE", "/api/v1/resource/1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "deleted ok") {
		t.Errorf("output missing raw response: %q", outBuf.String())
	}
}

func TestAPICmdDeleteNonJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "-X", "DELETE", "/api/v1/resource/1")
	if err == nil {
		t.Error("expected error for 404 DELETE response")
	}
}

func TestAPICmdDeleteJSONError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "forbidden"})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "api", "-X", "DELETE", "/api/v1/resource/1")
	if err == nil {
		t.Error("expected error for 403 DELETE response")
	}
	if !strings.Contains(err.Error(), "API error (403)") {
		t.Errorf("error = %q, want 'API error (403)'", err.Error())
	}
}
