package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

type capturedWrite struct {
	path   string
	method string
	key    string
	body   map[string]interface{}
}

// writeServer answers any POST with the given envelope and records the request.
func writeServer(t *testing.T, status int, envelope interface{}) (*httptest.Server, *capturedWrite) {
	t.Helper()
	got := &capturedWrite{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.EscapedPath()
		got.method = r.Method
		got.key = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func decisionEnvelope(decision string) map[string]interface{} {
	return map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"recommendation_id":     "REC-1234",
			"saving_acceptance":     decision,
			"saving_accepted_by":    "bruno@levelfour.ai",
			"saving_accepted_at":    "2026-08-21T10:00:00Z",
			"rejection_reason":      "operational",
			"rejection_explanation": "Owned by a migrating team",
		},
	}
}

func TestRecommendationDecisionCommands(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPath string
		wantBody map[string]interface{}
		wantOut  []string
	}{
		{
			name:     "accept",
			args:     []string{"rec", "accept", "REC-1234", "--yes"},
			wantPath: "/api/v1/recommendations/REC-1234/decision",
			wantBody: map[string]interface{}{"decision": "accepted"},
			wantOut:  []string{"REC-1234 accepted", "bruno@levelfour.ai"},
		},
		{
			name:     "reject without a reason",
			args:     []string{"rec", "reject", "REC-1234", "--yes"},
			wantPath: "/api/v1/recommendations/REC-1234/decision",
			wantBody: map[string]interface{}{"decision": "rejected"},
			wantOut:  []string{"REC-1234 rejected"},
		},
		{
			name:     "reject with a reason and explanation",
			args:     []string{"rec", "reject", "REC-1234", "--yes", "--reason", "other", "--explanation", "Owned by a migrating team"},
			wantPath: "/api/v1/recommendations/REC-1234/decision",
			wantBody: map[string]interface{}{
				"decision":    "rejected",
				"reason":      "other",
				"explanation": "Owned by a migrating team",
			},
			wantOut: []string{"REC-1234 rejected", "operational", "Owned by a migrating team"},
		},
		{
			name:     "id with a slash is escaped",
			args:     []string{"rec", "accept", "CLICK/243", "--yes"},
			wantPath: "/api/v1/recommendations/CLICK%2F243/decision",
			wantBody: map[string]interface{}{"decision": "accepted"},
			wantOut:  []string{"accepted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := writeServer(t, http.StatusOK, decisionEnvelope("accepted"))

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			outBuf, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.method != "POST" {
				t.Errorf("method = %q, want POST", got.method)
			}
			if got.path != tt.wantPath {
				t.Errorf("path = %q, want %q", got.path, tt.wantPath)
			}
			if got.key == "" {
				t.Error("expected an Idempotency-Key header")
			}
			if len(got.body) != len(tt.wantBody) {
				t.Errorf("body = %v, want %v", got.body, tt.wantBody)
			}
			for k, want := range tt.wantBody {
				if got.body[k] != want {
					t.Errorf("body[%q] = %v, want %v", k, got.body[k], want)
				}
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(outBuf.String(), want) {
					t.Errorf("output missing %q: %q", want, outBuf.String())
				}
			}
		})
	}
}

func TestRecommendationExecuteCommand(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantMethod string
		wantOut    []string
	}{
		{
			name:       "defaults to one-click",
			args:       []string{"rec", "execute", "REC-1234", "--yes"},
			wantMethod: "one-click",
			wantOut:    []string{"Execution requested", "REC-1234", "processing"},
		},
		{
			name:       "explicit iac method",
			args:       []string{"rec", "execute", "REC-1234", "--yes", "--method", "iac"},
			wantMethod: "iac",
			wantOut:    []string{"Execution requested"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := writeServer(t, http.StatusOK, map[string]interface{}{
				"success": true,
				"data": map[string]interface{}{
					"recommendation_id":     "REC-1234",
					"status":                "processing",
					"implementation_method": tt.wantMethod,
				},
			})

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			outBuf, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.path != "/api/v1/recommendations/audit/execution-requests" {
				t.Errorf("path = %q", got.path)
			}
			if got.key == "" {
				t.Error("expected an Idempotency-Key header")
			}
			if got.body["recommendation_id"] != "REC-1234" {
				t.Errorf("recommendation_id = %v", got.body["recommendation_id"])
			}
			if got.body["implementation_method"] != tt.wantMethod {
				t.Errorf("implementation_method = %v, want %q", got.body["implementation_method"], tt.wantMethod)
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(outBuf.String(), want) {
					t.Errorf("output missing %q: %q", want, outBuf.String())
				}
			}
		})
	}
}

func TestRecommendationWriteFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"bad reason", []string{"rec", "reject", "REC-1234", "--yes", "--reason", "because"}, `invalid --reason "because"`},
		{"bad method", []string{"rec", "execute", "REC-1234", "--yes", "--method", "magic"}, `invalid --method "magic"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.Write([]byte(`{"success":true,"data":{}}`))
			}))
			defer srv.Close()

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			_, _, err := executeCommand(t, tt.args...)
			if err == nil {
				t.Fatal("expected a validation error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
			if called {
				t.Error("an invalid flag must not reach the API")
			}
		})
	}
}

// A read-only key gets a 403. The message must send the user to mint a
// read-write key, not back through the login flow.
func TestRecommendationWriteForbidden(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"accept", []string{"rec", "accept", "REC-1234", "--yes"}},
		{"reject", []string{"rec", "reject", "REC-1234", "--yes"}},
		{"execute", []string{"rec", "execute", "REC-1234", "--yes"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"error":{"message":"API key scope 'read' is insufficient"}}`))
			}))
			defer srv.Close()

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			_, _, err := executeCommand(t, tt.args...)
			if err == nil {
				t.Fatal("expected an error for 403")
			}
			msg := err.Error()
			if !strings.Contains(msg, "permission denied") {
				t.Errorf("error = %q, want 'permission denied'", msg)
			}
			if !strings.Contains(msg, "read-write key") {
				t.Errorf("error = %q, want it to mention minting a read-write key", msg)
			}
			if strings.Contains(msg, "l4 auth login") {
				t.Errorf("a 403 must not tell the user to re-authenticate: %q", msg)
			}
		})
	}
}

func TestRecommendationWriteUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid token"}}`))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"

	_, _, err := executeCommand(t, "rec", "accept", "REC-1234", "--yes")
	if err == nil {
		t.Fatal("expected an error for 401")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %q, want 'authentication failed'", err.Error())
	}
	if strings.Contains(err.Error(), "permission denied") {
		t.Errorf("a 401 must not be reported as a permission problem: %q", err.Error())
	}
}

func TestRecommendationWriteConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		input      string
		wantCalled bool
		wantOut    string
	}{
		{"accept declined", []string{"rec", "accept", "REC-1234"}, "n\n", false, "Aborted."},
		{"accept confirmed", []string{"rec", "accept", "REC-1234"}, "y\n", true, "accepted"},
		{"execute declined", []string{"rec", "execute", "REC-1234"}, "n\n", false, "Aborted."},
		{"execute confirmed", []string{"rec", "execute", "REC-1234"}, "y\n", true, "Execution requested"},
		{"accept with --yes never prompts", []string{"rec", "accept", "REC-1234", "--yes"}, "n\n", true, "accepted"},
		{"execute with --yes never prompts", []string{"rec", "execute", "REC-1234", "--yes"}, "n\n", true, "Execution requested"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := writeServer(t, http.StatusOK, decisionEnvelope("accepted"))

			withTerminal(t, true)
			withStdin(t, tt.input)

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			outBuf, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			called := got.method != ""
			if called != tt.wantCalled {
				t.Errorf("API called = %v, want %v", called, tt.wantCalled)
			}
			if !strings.Contains(outBuf.String(), tt.wantOut) {
				t.Errorf("output missing %q: %q", tt.wantOut, outBuf.String())
			}
		})
	}
}

func TestRecommendationWriteJSONOutput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"accept", []string{"rec", "accept", "REC-1234", "--yes", "--json"}, "saving_acceptance"},
		{"execute", []string{"rec", "execute", "REC-1234", "--yes", "--json"}, "saving_acceptance"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := writeServer(t, http.StatusOK, decisionEnvelope("accepted"))

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			outBuf, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(outBuf.String(), tt.want) {
				t.Errorf("JSON output missing %q: %q", tt.want, outBuf.String())
			}
		})
	}
}

func TestRecommendationWriteEmptyData(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"accept", []string{"rec", "accept", "REC-1234", "--yes"}, "REC-1234 accepted"},
		{"execute", []string{"rec", "execute", "REC-1234", "--yes"}, "Execution requested"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"success": true, "data": map[string]interface{}{}})

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"

			outBuf, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(outBuf.String(), tt.want) {
				t.Errorf("output missing %q: %q", tt.want, outBuf.String())
			}
		})
	}
}

func TestRecommendationsAliases(t *testing.T) {
	for _, alias := range []string{"rec", "recs"} {
		if !slices.Contains(recommendationsCmd.Aliases, alias) {
			t.Errorf("recommendations command missing alias %q", alias)
		}
	}
}
