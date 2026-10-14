package cli

import (
	"encoding/json"
	"errors"
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

func TestAlertCreate(t *testing.T) {
	srv, got := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "create", "--file", ruleFile(t, ruleJSON), "--yes")
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

func TestAlertCreateFromStdin(t *testing.T) {
	srv, got := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	original := stdinReader
	stdinReader = strings.NewReader(ruleJSON)
	t.Cleanup(func() { stdinReader = original })

	_, _, err := executeCommand(t, "alerts", "create", "--file", "-", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.body["name"] != "Production budget" {
		t.Errorf("body = %v", got.body)
	}
}

func TestAlertCreateJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusCreated, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "create", "--file", ruleFile(t, ruleJSON), "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"id\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestAlertCreateRefusedByTheAPI(t *testing.T) {
	srv, _ := writeServer(t, http.StatusUnprocessableEntity, map[string]interface{}{
		"detail": "An alert with no recipients and no Slack channel would notify nobody",
	})
	useServer(t, srv)

	_, _, err := executeCommand(t, "alerts", "create", "--file", ruleFile(t, ruleJSON), "--yes")
	if err == nil || !strings.Contains(err.Error(), "would notify nobody") {
		t.Fatalf("err = %v, want the API's reason", err)
	}
}

func TestAlertCreateFileProblems(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no file at all",
			args: []string{"alerts", "create", "--yes"},
			want: "--file is required",
		},
		{
			name: "a path that is not there",
			args: []string{"alerts", "create", "--file", "/nowhere/rule.json", "--yes"},
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

	t.Run("a file that holds null", func(t *testing.T) {
		_, _, err := executeCommand(t, "alerts", "create", "--file", ruleFile(t, "null"), "--yes")
		if err == nil || !strings.Contains(err.Error(), "it holds null") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a file that is not a JSON object", func(t *testing.T) {
		_, _, err := executeCommand(t, "alerts", "create", "--file", ruleFile(t, "[]"), "--yes")
		if err == nil || !strings.Contains(err.Error(), "not a JSON object") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestAlertPauseAndResume(t *testing.T) {
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

			out, _, err := executeCommand(t, "alerts", tt.verb, testAlertID, "--yes")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.method != "PATCH" || got.path != "/api/v1/cost-alerts/"+testAlertID {
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

func TestAlertPauseJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(false)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "pause", testAlertID, "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"enabled\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestAlertPauseUnknownAlert(t *testing.T) {
	srv, _ := writeServer(t, http.StatusNotFound, map[string]interface{}{"detail": "Alert not found: " + testAlertID})
	useServer(t, srv)

	_, _, err := executeCommand(t, "alerts", "pause", testAlertID, "--yes")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestAlertDelete(t *testing.T) {
	var method, path string
	var sentBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.EscapedPath()
		sentBody, _ = io.ReadAll(r.Body)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"id": testAlertID, "deleted": true},
		})
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "delete", testAlertID, "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if method != "DELETE" || path != "/api/v1/cost-alerts/"+testAlertID {
		t.Errorf("%s %s", method, path)
	}
	if len(sentBody) != 0 {
		t.Errorf("DELETE carried a body: %q", sentBody)
	}
	if !strings.Contains(out.String(), testAlertID+" deleted") {
		t.Errorf("output = %q", out.String())
	}
}

func TestAlertDeleteJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"data": map[string]interface{}{"deleted": true}})
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "delete", testAlertID, "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"deleted\"") {
		t.Errorf("output = %q", out.String())
	}
}

// Without --yes on a terminal the prompt decides, and a declined write sends nothing.
func TestAlertWritesStopOnADeclinedPrompt(t *testing.T) {
	tests := []struct {
		name string
		args func(t *testing.T) []string
	}{
		{name: "create", args: func(t *testing.T) []string {
			return []string{"alerts", "create", "--file", ruleFile(t, ruleJSON)}
		}},
		{name: "update", args: func(t *testing.T) []string {
			return []string{"alerts", "update", testAlertID, "-f", ruleFile(t, `{"name": "Renamed"}`)}
		}},
		{name: "pause", args: func(*testing.T) []string { return []string{"alerts", "pause", testAlertID} }},
		{name: "delete", args: func(*testing.T) []string { return []string{"alerts", "delete", testAlertID} }},
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
			withTerminal(t, true)
			withStdin(t, "n\n")

			out, _, err := executeCommand(t, tt.args(t)...)
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

func TestAlertUpdate(t *testing.T) {
	changed := alertBody(true)
	changed["params"] = map[string]interface{}{"kind": "budget", "budget": 50000}
	srv, got := writeServer(t, http.StatusOK, map[string]interface{}{"data": changed})
	useServer(t, srv)

	change := `{"params": {"kind": "budget", "budget": 50000, "reset": "monthly", "rules": [{"amount": 80, "unit": "percent"}]}}`
	out, _, err := executeCommand(t, "alerts", "update", testAlertID, "-f", ruleFile(t, change), "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.method != "PATCH" || got.path != "/api/v1/cost-alerts/"+testAlertID {
		t.Errorf("%s %s", got.method, got.path)
	}
	if got.key == "" {
		t.Error("expected an Idempotency-Key header")
	}
	if _, ok := got.body["params"]; !ok || len(got.body) != 1 {
		t.Errorf("only the fields in the file should be sent: %v", got.body)
	}
	for _, want := range []string{testAlertID + " changed", "50000", "#finops (C0FINOPS01)"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestAlertUpdateJSON(t *testing.T) {
	srv, _ := writeServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "update", testAlertID, "-f", ruleFile(t, `{"name": "Renamed"}`), "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"source_id\"") {
		t.Errorf("output = %q", out.String())
	}
}

// An empty object would read the rule back unchanged, which is never what was meant.
func TestAlertUpdateRefusesAFileThatChangesNothing(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)

	_, _, err := executeCommand(t, "alerts", "update", testAlertID, "-f", ruleFile(t, "{}"), "--yes")
	if err == nil || !strings.Contains(err.Error(), "changes nothing") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Error("an empty change still reached the API")
	}
}

func TestAlertUpdateRefusedByTheAPI(t *testing.T) {
	srv, _ := writeServer(t, http.StatusUnprocessableEntity, map[string]interface{}{
		"detail": "LevelFour cannot see the Slack channel G0LEADERSH.",
	})
	useServer(t, srv)

	_, _, err := executeCommand(t, "alerts", "update", testAlertID, "-f", ruleFile(t, `{"slack_channel_ids": ["G0LEADERSH"]}`), "--yes")
	if err == nil || !strings.Contains(err.Error(), "cannot see the Slack channel") {
		t.Fatalf("err = %v, want the API's reason", err)
	}
}

func TestAlertUpdateNeedsAFile(t *testing.T) {
	_, _, err := executeCommand(t, "alerts", "update", testAlertID, "--yes")
	if err == nil || !strings.Contains(err.Error(), "--file is required") {
		t.Fatalf("err = %v", err)
	}
}

// Nothing takes a delete back, so a script has to say --yes rather than inherit a yes.
func TestAlertDeleteOutsideATerminalNeedsYes(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)
	withTerminal(t, false)

	_, _, err := executeCommand(t, "alerts", "delete", testAlertID)
	if err == nil || !strings.Contains(err.Error(), "deleting "+testAlertID+" outside a terminal needs --yes") {
		t.Fatalf("err = %v", err)
	}
	if called {
		t.Error("an unattended delete without --yes still reached the API")
	}
}

func TestAlertDeleteUnknownAlert(t *testing.T) {
	srv, _ := writeServer(t, http.StatusNotFound, map[string]interface{}{"detail": "Alert not found: " + testAlertID})
	useServer(t, srv)

	_, _, err := executeCommand(t, "alerts", "delete", testAlertID, "--yes")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

// Piped in from a terminal, the prompt would read its answer from the stdin the rule already used.
func TestAlertWriteFromStdinNeedsYesAtATerminal(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)
	withTerminal(t, true)

	for _, args := range [][]string{
		{"alerts", "create", "--file", "-"},
		{"alerts", "update", testAlertID, "--file", "-"},
	} {
		withStdin(t, ruleJSON)
		_, _, err := executeCommand(t, args...)
		if !errors.Is(err, errStdinTaken) {
			t.Errorf("%v: err = %v", args, err)
		}
	}
	if called {
		t.Error("a write with no way to answer its prompt still reached the API")
	}
}

func TestAlertFileOverTheCapIsRefused(t *testing.T) {
	huge := `{"name": "` + strings.Repeat("a", maxAlertFileBytes) + `"}`
	for name, args := range map[string][]string{
		"file":  {"alerts", "create", "--file", ruleFile(t, huge), "--yes"},
		"stdin": {"alerts", "create", "--file", "-", "--yes"},
	} {
		t.Run(name, func(t *testing.T) {
			withStdin(t, huge)
			_, _, err := executeCommand(t, args...)
			if err == nil || !strings.Contains(err.Error(), "larger than") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
