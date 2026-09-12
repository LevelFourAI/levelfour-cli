package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	kr "github.com/zalando/go-keyring"
)

func withTerminal(t *testing.T, tty bool) {
	t.Helper()
	orig, origPrompt := isTerminal, canPrompt
	isTerminal = func() bool { return tty }
	canPrompt = func() bool { return tty }
	t.Cleanup(func() { isTerminal, canPrompt = orig, origPrompt })
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	orig := stdinReader
	stdinReader = strings.NewReader(input)
	t.Cleanup(func() { stdinReader = orig })
}

func TestConfirmAction(t *testing.T) {
	tests := []struct {
		name  string
		tty   bool
		input string
		want  bool
	}{
		{"non tty skips the prompt", false, "", true},
		{"y confirms", true, "y\n", true},
		{"yes confirms", true, "YES\n", true},
		{"n declines", true, "n\n", false},
		{"empty declines", true, "\n", false},
		{"eof declines", true, "", false},
		{"anything else declines", true, "maybe\n", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			captureOutput(t)
			withTerminal(t, tt.tty)
			withStdin(t, tt.input)

			if got := confirmAction("Accept recommendation REC-1?"); got != tt.want {
				t.Errorf("confirmAction() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfirmActionPrintsPrompt(t *testing.T) {
	outBuf, _ := captureOutput(t)
	withTerminal(t, true)
	withStdin(t, "y\n")

	confirmAction("Accept recommendation REC-1?")

	if !strings.Contains(outBuf.String(), "Accept recommendation REC-1? [y/N]:") {
		t.Errorf("prompt missing from output: %q", outBuf.String())
	}
}

func TestPostWriteSendsIdempotencyKey(t *testing.T) {
	var gotKey, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		gotMethod = r.Method
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Write([]byte(`{"success":true,"data":{"ok":"yes"}}`))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	envelope, err := postWrite("/api/v1/thing", map[string]string{"decision": "accepted"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotKey == "" {
		t.Error("expected an Idempotency-Key header")
	}
	if !strings.Contains(gotBody, `"decision":"accepted"`) {
		t.Errorf("body = %q, want the decision field", gotBody)
	}
	if envelopeData(envelope)["ok"] != "yes" {
		t.Errorf("envelope = %v, want data.ok = yes", envelope)
	}
}

func TestPostWriteErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{"403 reports a permission problem", http.StatusForbidden, `{"error":{"message":"forbidden"}}`, "permission denied"},
		{"401 reports an auth problem", http.StatusUnauthorized, `{"error":{"message":"unauthorized"}}`, "authentication failed"},
		{"404 passes through", http.StatusNotFound, `{"error":{"message":"no such recommendation"}}`, "no such recommendation"},
		{"invalid json body", http.StatusOK, `not json`, "invalid JSON response"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"
			defer resetFlags()

			_, err := postWrite("/api/v1/thing", map[string]string{})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestPostWriteUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	flagAPI = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, err := postWrite("/api/v1/thing", nil)
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error = %v, want 'not authenticated'", err)
	}
}

func TestPostWriteTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, err := postWrite("/api/v1/thing", nil)
	if err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestEnvelopeDataAndDataString(t *testing.T) {
	tests := []struct {
		name     string
		envelope map[string]interface{}
		key      string
		want     string
	}{
		{"string field", map[string]interface{}{"data": map[string]interface{}{"status": "processing"}}, "status", "processing"},
		{"missing field", map[string]interface{}{"data": map[string]interface{}{}}, "status", ""},
		{"non string field", map[string]interface{}{"data": map[string]interface{}{"status": 7}}, "status", ""},
		{"data is not an object", map[string]interface{}{"data": "nope"}, "status", ""},
		{"no data key", map[string]interface{}{}, "status", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dataString(envelopeData(tt.envelope), tt.key); got != tt.want {
				t.Errorf("dataString() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequestHelpersMethodsAndKeys(t *testing.T) {
	payload := map[string]string{"name": "Teams"}
	tests := []struct {
		name     string
		send     func(string) error
		method   string
		wantKey  bool
		wantBody bool
	}{
		{"post", func(p string) error { _, err := postWrite(p, payload); return err }, http.MethodPost, true, true},
		{"put", func(p string) error { _, err := putWrite(p, payload); return err }, http.MethodPut, true, true},
		{"patch", func(p string) error { _, err := patchWrite(p, payload); return err }, http.MethodPatch, true, true},
		{"delete", deleteWrite, http.MethodDelete, false, false},
		{"get", func(p string) error { _, err := getJSON(p); return err }, http.MethodGet, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotKey, gotBody string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotKey = r.Method, r.Header.Get("Idempotency-Key")
				raw, _ := io.ReadAll(r.Body)
				gotBody = string(raw)
				w.Write([]byte(`{"success":true,"data":{}}`))
			}))
			defer srv.Close()

			flagAPI = srv.URL
			flagToken = "l4_test_testkey123456789a"
			defer resetFlags()

			if err := tt.send("/api/v1/thing"); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != tt.method {
				t.Errorf("method = %q, want %q", gotMethod, tt.method)
			}
			if (gotKey != "") != tt.wantKey {
				t.Errorf("Idempotency-Key = %q, want present: %v", gotKey, tt.wantKey)
			}
			if strings.Contains(gotBody, `"name":"Teams"`) != tt.wantBody {
				t.Errorf("body = %q, want the payload: %v", gotBody, tt.wantBody)
			}
		})
	}
}

func TestGetJSONInvalidBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	if _, err := getJSON("/api/v1/thing"); err == nil || !strings.Contains(err.Error(), "invalid JSON response") {
		t.Errorf("error = %v, want 'invalid JSON response'", err)
	}
}

func TestDescribeAPIError(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"message only", `{"error":{"message":"gone"}}`, "API error (422): gone"},
		{"not json", `oops`, "API error (422): oops"},
		{
			"service problems",
			`{"error":{"code":"validation_failed","message":"bad","details":{"problems":[{"code":"no_values"},{"code":"split_not_100","config_index":1,"split_index":2},"junk"]}}}`,
			"API error (422): bad\n  - no_values\n  - split_not_100 at config 2, split 3",
		},
		{
			"request validation",
			`{"error":{"code":"VALIDATION_ERROR","message":"Invalid request data","details":[{"loc":["body","configs",0,"assign"],"msg":"Field required"},"junk"]}}`,
			"API error (422): Invalid request data\n  - body.configs.0.assign: Field required",
		},
		{
			"dependents in every shape",
			`{"error":{"code":"has_dependents","message":"in use","details":{"dependents":[{"id":"vtk_1"},{"name":"Business Units"},"Other"]}}}`,
			"API error (422): in use\nRead by: vtk_1, Business Units, Other",
		},
		{
			"version conflict names the version it collided with",
			`{"error":{"code":"version_conflict","message":"stale","details":{"current_version":5}}}`,
			"API error (422): stale\nIt is now at version 5. " + versionConflictHint,
		},
		{
			"version conflict with no version still says what to do",
			`{"error":{"code":"version_conflict","message":"stale","details":{}}}`,
			"API error (422): stale\n" + versionConflictHint,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := describeAPIError(&api.RawResponse{StatusCode: http.StatusUnprocessableEntity, Body: []byte(tt.body)})
			if got.Error() != tt.want {
				t.Errorf("describeAPIError() =\n%s\nwant\n%s", got.Error(), tt.want)
			}
		})
	}
}
