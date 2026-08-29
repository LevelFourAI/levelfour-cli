package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func withTerminal(t *testing.T, tty bool) {
	t.Helper()
	orig := isTerminal
	isTerminal = func() bool { return tty }
	t.Cleanup(func() { isTerminal = orig })
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
