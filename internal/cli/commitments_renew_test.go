package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	kr "github.com/zalando/go-keyring"
)

const renewalRoutePrefix = api.CommitmentsPath + "/renewal/"

type renewCalls struct {
	reads int
	write capturedWrite
}

func renewServer(t *testing.T, priorBody string, reply apiReply) *renewCalls {
	t.Helper()
	calls := &renewCalls{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, renewalRoutePrefix) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method == http.MethodGet {
			calls.reads++
			answerPriorRenewal(w, priorBody)
			return
		}
		calls.write.path = r.URL.EscapedPath()
		calls.write.method = r.Method
		calls.write.key = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &calls.write.body)
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	useCommitmentsServer(t, srv)
	return calls
}

func answerPriorRenewal(w http.ResponseWriter, priorBody string) {
	if priorBody == "" {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"success":false,"error":{"code":"NOT_FOUND","message":"No renewal raised"}}`)
		return
	}
	_, _ = io.WriteString(w, priorBody)
}

func renewalEnvelope(fields string) string {
	return `{"success":true,"data":{"commitment_id":"ri-0a1b2c3d","recommendation_id":"RENEW-12",` + fields + `}}`
}

func raised(fields string) apiReply {
	return apiReply{status: http.StatusCreated, body: renewalEnvelope(fields)}
}

func TestCommitmentsRenewReportsWhatTheRaiseDid(t *testing.T) {
	closedPrior := `{"data":{"commitment_id":"ri-0a1b2c3d","recommendation_id":"RENEW-11","closed":true}}`
	openPrior := `{"data":{"commitment_id":"ri-0a1b2c3d","recommendation_id":"RENEW-12","rebindable":true}}`
	tests := []struct {
		name     string
		prior    string
		reply    apiReply
		args     []string
		wantBody map[string]interface{}
		want     []string
		unwanted []string
	}{
		{
			name: "created",
			reply: raised(`"created":true,"rebindable":true,"offering_id":"offer-a","quantity":36.0,` +
				`"blocking_changes":[]`),
			wantBody: map[string]interface{}{},
			want: []string{"Raised RENEW-12 for ri-0a1b2c3d", "offer-a", "36", "yes, until someone accepts",
				"Nothing is bought until an organization admin releases it",
				"l4 rec accept RENEW-12", "l4 rec request RENEW-12 --method one-click"},
			unwanted: []string{"Accepted changes still to land"},
		},
		{
			name:     "rebound to the picked option",
			prior:    openPrior,
			reply:    raised(`"rebound":true,"rebindable":true,"offering_id":"offer-b","quantity":12.5`),
			args:     []string{"--offering", "offer-b", "--quantity", "12.5"},
			wantBody: map[string]interface{}{"offering_id": "offer-b", "quantity": 12.5},
			want:     []string{"RENEW-12 now buys the option you picked", "offer-b", "12.5"},
		},
		{
			name:     "an offering alone takes its sized quantity",
			prior:    openPrior,
			reply:    raised(`"rebound":true,"rebindable":true,"offering_id":"offer-b","quantity":36.0`),
			args:     []string{"--offering", "offer-b"},
			wantBody: map[string]interface{}{"offering_id": "offer-b"},
			want:     []string{"now buys the option you picked"},
		},
		{
			name:     "unchanged once decided",
			prior:    openPrior,
			reply:    raised(`"rebindable":false,"offering_id":null,"quantity":null`),
			wantBody: map[string]interface{}{},
			want:     []string{"RENEW-12 was already raised for ri-0a1b2c3d. Nothing changed", "no, it has been decided", "Offering: none"},
			unwanted: []string{"l4 rec accept RENEW-12"},
		},
		{
			name:     "raised again after a rejection",
			prior:    closedPrior,
			reply:    raised(`"created":true,"rebindable":true,"offering_id":"offer-a","quantity":36.0`),
			wantBody: map[string]interface{}{},
			want:     []string{"Raised RENEW-12 for ri-0a1b2c3d, replacing RENEW-11, which was rejected"},
		},
		{
			name: "accepted changes still to land",
			reply: raised(`"created":true,"rebindable":true,"offering_id":"offer-a","quantity":36.0,` +
				`"blocking_changes":[{"recommendation_id":"REC-1234","service":"RDS","account":"prod",` +
				`"monthly_savings":210,"status":"accepted"}]`),
			wantBody: map[string]interface{}{},
			want:     []string{"Accepted changes still to land", "REC-1234", "$210.00/mo", "sizing does not count on it"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := renewServer(t, tt.prior, tt.reply)

			args := append([]string{"commitments", "renew", "ri-0a1b2c3d", "--yes"}, tt.args...)
			out, _, err := executeCommand(t, args...)
			if err != nil {
				t.Fatalf("renew error: %v", err)
			}
			if calls.write.method != http.MethodPost || calls.write.path != renewalRoutePrefix+"ri-0a1b2c3d" {
				t.Errorf("raise = %s %s, want POST %sri-0a1b2c3d", calls.write.method, calls.write.path, renewalRoutePrefix)
			}
			if calls.write.key == "" {
				t.Error("expected an Idempotency-Key header")
			}
			assertBody(t, calls.write.body, tt.wantBody)
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(out.String(), unwanted) {
					t.Errorf("output should not contain %q:\n%s", unwanted, out.String())
				}
			}
		})
	}
}

func assertBody(t *testing.T, got, want map[string]interface{}) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("body = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("body[%q] = %v, want %v", k, got[k], v)
		}
	}
}

func TestCommitmentsRenewEscapesAnARNIntoThePath(t *testing.T) {
	calls := renewServer(t, "", raised(`"created":true,"rebindable":true`))
	arn := "arn:aws:savingsplans::111122223333:savingsplan/abc"

	if _, _, err := executeCommand(t, "commitments", "renew", arn, "--yes"); err != nil {
		t.Fatalf("renew error: %v", err)
	}
	if want := renewalRoutePrefix + "arn:aws:savingsplans::111122223333:savingsplan%2Fabc"; calls.write.path != want {
		t.Errorf("path = %q, want %q", calls.write.path, want)
	}
}

func TestCommitmentsRenewRefusesAQuantityTheAPIWouldRefuse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"quantity without an offering", []string{"--quantity", "4"}, "--quantity is priced per offering, so it needs --offering"},
		{"zero quantity", []string{"--offering", "offer-a", "--quantity", "0"}, "invalid --quantity 0: it must be above zero"},
		{"negative quantity", []string{"--offering", "offer-a", "--quantity", "-2.5"}, "invalid --quantity -2.5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := renewServer(t, "", raised(`"created":true`))

			args := append([]string{"commitments", "renew", "ri-0a1b2c3d", "--yes"}, tt.args...)
			_, _, err := executeCommand(t, args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if calls.reads != 0 || calls.write.method != "" {
				t.Error("a refused pick must not reach the API")
			}
		})
	}
}

func TestCommitmentsRenewConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		tty        bool
		input      string
		args       []string
		wantRaised bool
		wantOut    string
		wantErr    string
	}{
		{name: "confirmed", tty: true, input: "y\n", wantRaised: true, wantOut: "Raised RENEW-12"},
		{name: "declined", tty: true, input: "n\n", wantOut: "Aborted."},
		{name: "--yes skips the prompt", tty: true, input: "n\n", args: []string{"--yes"}, wantRaised: true, wantOut: "Raised RENEW-12"},
		{name: "unattended with --yes", args: []string{"-y"}, wantRaised: true, wantOut: "Raised RENEW-12"},
		{name: "unattended without --yes", wantErr: "raising a renewal for ri-0a1b2c3d outside a terminal needs --yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := renewServer(t, "", raised(`"created":true,"rebindable":true`))
			withTerminal(t, tt.tty)
			withStdin(t, tt.input)

			args := append([]string{"commitments", "renew", "ri-0a1b2c3d"}, tt.args...)
			out, _, err := executeCommand(t, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("renew error: %v", err)
			}
			if raisedIt := calls.write.method != ""; raisedIt != tt.wantRaised {
				t.Errorf("raised = %v, want %v", raisedIt, tt.wantRaised)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output missing %q:\n%s", tt.wantOut, out.String())
			}
		})
	}
}

func TestCommitmentsRenewPrintsTheEnvelopeAsJSON(t *testing.T) {
	calls := renewServer(t, "", raised(`"created":true,"rebindable":true,"closed":false`))

	out, _, err := executeCommand(t, "commitments", "renew", "ri-0a1b2c3d", "--yes", "--json")
	if err != nil {
		t.Fatalf("renew error: %v", err)
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if envelopeData(envelope)["rebindable"] != true {
		t.Errorf("JSON output lost a field: %s", out.String())
	}
	if calls.reads != 0 {
		t.Errorf("JSON output read the earlier renewal %d times, want none", calls.reads)
	}
}

func TestCommitmentsRenewAsJSONReportsARefusal(t *testing.T) {
	renewServer(t, "", apiReply{http.StatusConflict,
		`{"success":false,"error":{"code":"CONFLICT","message":"This renewal has already been decided"}}`})

	out, _, err := executeCommand(t, "commitments", "renew", "ri-0a1b2c3d", "--yes", "--json")
	if err == nil || !strings.Contains(err.Error(), "already been decided") {
		t.Fatalf("error = %v, want the refusal", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refusal printed a result: %s", out.String())
	}
}

func TestCommitmentsRenewReportsRefusals(t *testing.T) {
	tests := []struct {
		name     string
		reply    apiReply
		want     []string
		unwanted string
	}{
		{
			name: "decided on another pick",
			reply: apiReply{http.StatusConflict, `{"success":false,"error":{"code":"CONFLICT",` +
				`"message":"This renewal has already been decided, so what it buys can no longer change"}}`},
			want: []string{"already been decided, so what it buys can no longer change"},
		},
		{
			name: "read-only key",
			reply: apiReply{http.StatusForbidden,
				`{"success":false,"error":{"code":"AUTHORIZATION_ERROR","message":"API key scope insufficient"}}`},
			want:     []string{"permission denied", "read-write key", credentialEnvVar},
			unwanted: "l4 auth login",
		},
		{
			name:  "a reply that is not JSON",
			reply: apiReply{http.StatusCreated, `not json`},
			want:  []string{"invalid JSON response"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renewServer(t, "", tt.reply)

			_, _, err := executeCommand(t, "commitments", "renew", "ri-0a1b2c3d", "--yes")
			if err == nil {
				t.Fatal("expected the refusal as an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), want)
				}
			}
			if tt.unwanted != "" && strings.Contains(err.Error(), tt.unwanted) {
				t.Errorf("error = %q, must not contain %q", err.Error(), tt.unwanted)
			}
		})
	}
}

func TestCommitmentsRenewStillRaisesWhenTheEarlierRenewalCannotBeRead(t *testing.T) {
	raisedIt := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":{"message":"database unavailable"}}`)
			return
		}
		raisedIt = true
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, renewalEnvelope(`"created":true,"rebindable":true`))
	}))
	t.Cleanup(srv.Close)
	useCommitmentsServer(t, srv)

	out, _, err := executeCommand(t, "commitments", "renew", "ri-0a1b2c3d", "--yes")
	if err != nil {
		t.Fatalf("renew error: %v", err)
	}
	if !raisedIt || !strings.Contains(out.String(), "Raised RENEW-12 for ri-0a1b2c3d") {
		t.Errorf("expected the raise despite the failed read:\n%s", out.String())
	}
}

func TestCommitmentsRenewUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv(credentialEnvVar, "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "renew", "ri-0a1b2c3d", "--yes"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
