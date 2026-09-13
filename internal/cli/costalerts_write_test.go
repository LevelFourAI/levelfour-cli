package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ruleJSON = `{
  "name": "Production budget",
  "params": {"kind": "budget", "budget": 40000, "reset": "monthly", "rules": [{"amount": 80, "unit": "percent"}]},
  "source_id": "123456789012",
  "recipients": [{"name": "Ana", "email": "ana@example.com", "member": true}]
}`

func ruleFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rule.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("cannot write the fixture: %v", err)
	}
	return path
}

func TestCostAlertCreate(t *testing.T) {
	srv, got := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "create", "--file", ruleFile(t, ruleJSON), "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.method != "POST" || got.path != "/api/v1/cost-alerts" {
		t.Errorf("%s %s", got.method, got.path)
	}
	if got.key == "" {
		t.Error("expected an Idempotency-Key header")
	}
	if got.body["source_id"] != "123456789012" {
		t.Errorf("body = %v", got.body)
	}
	for _, want := range []string{"Cost alert created", "Production budget"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestCostAlertCreateFromStdin(t *testing.T) {
	srv, got := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	original := stdinReader
	stdinReader = strings.NewReader(ruleJSON)
	t.Cleanup(func() { stdinReader = original })

	_, _, err := executeCommand(t, "costalerts", "create", "--file", "-", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.body["name"] != "Production budget" {
		t.Errorf("body = %v", got.body)
	}
}

func TestCostAlertCreateJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "create", "--file", ruleFile(t, ruleJSON), "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"id\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertCreateRefusedByTheAPI(t *testing.T) {
	srv, _ := writeServer(t, http.StatusUnprocessableEntity, map[string]interface{}{
		"detail": "An alert with no recipients and no Slack channel would notify nobody",
	})
	useServer(t, srv)

	_, _, err := executeCommand(t, "costalerts", "create", "--file", ruleFile(t, ruleJSON), "--yes")
	if err == nil || !strings.Contains(err.Error(), "would notify nobody") {
		t.Fatalf("err = %v, want the API's reason", err)
	}
}

func TestCostAlertCreateFileProblems(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no file at all",
			args: []string{"costalerts", "create", "--yes"},
			want: "--file is required",
		},
		{
			name: "a path that is not there",
			args: []string{"costalerts", "create", "--file", "/nowhere/rule.json", "--yes"},
			want: "cannot read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := executeCommand(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}

	t.Run("a file that is not a JSON object", func(t *testing.T) {
		_, _, err := executeCommand(t, "costalerts", "create", "--file", ruleFile(t, "[]"), "--yes")
		if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCostAlertPauseAndResume(t *testing.T) {
	tests := []struct {
		name        string
		verb        string
		wantEnabled bool
		wantOut     string
	}{
		{name: "pause", verb: "pause", wantEnabled: false, wantOut: "paused"},
		{name: "resume", verb: "resume", wantEnabled: true, wantOut: "resumed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, got := writeServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(tt.wantEnabled)})
			useServer(t, srv)

			out, _, err := executeCommand(t, "costalerts", tt.verb, "alert-1", "--yes")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.method != "PATCH" || got.path != "/api/v1/cost-alerts/alert-1" {
				t.Errorf("%s %s", got.method, got.path)
			}
			if got.body["enabled"] != tt.wantEnabled {
				t.Errorf("enabled = %v, want %v", got.body["enabled"], tt.wantEnabled)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output missing %q: %q", tt.wantOut, out.String())
			}
		})
	}
}

func TestCostAlertPauseJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(false)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "pause", "alert-1", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"enabled\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertPauseUnknownAlert(t *testing.T) {
	srv, _ := writeServer(t, http.StatusNotFound, map[string]interface{}{"detail": "Alert not found: alert-1"})
	useServer(t, srv)

	_, _, err := executeCommand(t, "costalerts", "pause", "alert-1", "--yes")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestCostAlertDelete(t *testing.T) {
	var method, path string
	var sentBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.EscapedPath()
		sentBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"id": "alert-1", "deleted": true},
		})
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "delete", "alert-1", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "DELETE" || path != "/api/v1/cost-alerts/alert-1" {
		t.Errorf("%s %s", method, path)
	}
	if len(sentBody) != 0 {
		t.Errorf("DELETE carried a body: %q", sentBody)
	}
	if !strings.Contains(out.String(), "alert-1 deleted") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertDeleteJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"deleted": true}})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "delete", "alert-1", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"deleted\"") {
		t.Errorf("output = %q", out.String())
	}
}

// Without --yes on a terminal the prompt decides, and a declined write sends nothing.
func TestCostAlertWritesStopOnADeclinedPrompt(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "create", args: []string{"costalerts", "create", "--file", "rule.json"}},
		{name: "pause", args: []string{"costalerts", "pause", "alert-1"}},
		{name: "delete", args: []string{"costalerts", "delete", "alert-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var called bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)
			useServer(t, srv)

			args := tt.args
			if tt.name == "create" {
				args = []string{"costalerts", "create", "--file", ruleFile(t, ruleJSON)}
			}

			originalTerminal := isTerminal
			isTerminal = func() bool { return true }
			t.Cleanup(func() { isTerminal = originalTerminal })
			original := stdinReader
			stdinReader = strings.NewReader("n\n")
			t.Cleanup(func() { stdinReader = original })

			out, _, err := executeCommand(t, args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if called {
				t.Error("a declined prompt still sent the request")
			}
			if !strings.Contains(out.String(), "Aborted") {
				t.Errorf("output = %q", out.String())
			}
		})
	}
}
