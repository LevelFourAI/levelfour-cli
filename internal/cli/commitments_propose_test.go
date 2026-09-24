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

const purchasePath = api.CommitmentsPath + "/purchase"

func proposeServer(t *testing.T, reply apiReply) *capturedWrite {
	t.Helper()
	got := &capturedWrite{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.method, got.key = r.URL.Path, r.Method, r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got.body)
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	useCommitmentsServer(t, srv)
	return got
}

func purchased(fields string) apiReply {
	return apiReply{status: http.StatusCreated, body: `{"success":true,"data":{"recommendation_id":"BUY-12",` +
		`"payer_account_id":"111122223333","term_months":12,"payment_option":"no_upfront",` +
		`"grain":"daily","source":"cur",` + fields + `}}`}
}

func TestCommitmentsProposeRaisesTheProposal(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		reply    apiReply
		wantBody map[string]interface{}
		want     []string
	}{
		{
			name: "balanced when nothing is named",
			args: []string{"--type", "compute"},
			reply: purchased(`"plan_type":"compute","commitment_hourly":4.1,"profile":"balanced",` +
				`"capped_by":[],"monthly_savings":1650,"created":true`),
			wantBody: map[string]interface{}{"plan_type": "compute"},
			want: []string{"Raised BUY-12", "111122223333", "Compute Savings Plan, 12 months, No Upfront",
				"$4.100/hr", "Balanced", "none", "$1650.00/mo", "Raising it bought nothing",
				"nothing is bought until an organization admin releases it",
				"l4 rec accept BUY-12", "l4 rec request BUY-12 --method manual, to buy the plan yourself"},
		},
		{
			name: "a profile on a named payer",
			args: []string{"--type", "compute", "--profile", "max_savings", "--payer", "111122223333"},
			reply: purchased(`"plan_type":"compute","commitment_hourly":5,"profile":"max_savings",` +
				`"capped_by":["aws_cap"],"monthly_savings":1900,"created":true`),
			wantBody: map[string]interface{}{"plan_type": "compute", "profile": "max_savings",
				"payer_account_id": "111122223333"},
			want: []string{"Max savings", "AWS recommendation", "$5.000/hr"},
		},
		{
			name: "a size of your own",
			args: []string{"--type", "database", "--commitment", "1.25"},
			reply: purchased(`"plan_type":"database","commitment_hourly":1.25,"profile":null,` +
				`"capped_by":[],"monthly_savings":210,"created":true`),
			wantBody: map[string]interface{}{"plan_type": "database", "commitment_hourly": 1.25},
			want:     []string{"Database Savings Plan", "your own size", "$1.250/hr"},
		},
		{
			name: "already raised on the same pick",
			args: []string{"--type", "compute"},
			reply: purchased(`"plan_type":"compute","commitment_hourly":4.1,"profile":"balanced",` +
				`"capped_by":[],"monthly_savings":1650,"created":false`),
			wantBody: map[string]interface{}{"plan_type": "compute"},
			want:     []string{"BUY-12 was already raised on this pick. Nothing changed", "l4 rec accept BUY-12"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proposeServer(t, tt.reply)

			args := append([]string{"commitments", "propose", "--yes"}, tt.args...)
			out, _, err := executeCommand(t, args...)
			if err != nil {
				t.Fatalf("propose error: %v", err)
			}
			if got.method != http.MethodPost || got.path != purchasePath {
				t.Errorf("request = %s %s, want POST %s", got.method, got.path, purchasePath)
			}
			if got.key == "" {
				t.Error("expected an Idempotency-Key header")
			}
			assertBody(t, got.body, tt.wantBody)
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestCommitmentsProposeRefusesAPickTheAPIWouldRefuse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no type", nil, "--type is required: choose one of compute, database"},
		{"unknown type", []string{"--type", "gpu"}, `invalid --type "gpu"`},
		{"unknown profile", []string{"--type", "compute", "--profile", "bold"}, `invalid --profile "bold"`},
		{"a profile and a commitment", []string{"--type", "compute", "--profile", "balanced", "--commitment", "2"},
			"name --profile or --commitment, not both"},
		{"zero commitment", []string{"--type", "compute", "--commitment", "0"}, "invalid --commitment 0: it must be above zero"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proposeServer(t, purchased(`"created":true`))

			_, _, err := executeCommand(t, append([]string{"commitments", "propose", "--yes"}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if got.method != "" {
				t.Error("a refused pick must not reach the API")
			}
		})
	}
}

func TestCommitmentsProposeConfirmation(t *testing.T) {
	tests := []struct {
		name       string
		tty        bool
		input      string
		args       []string
		wantRaised bool
		wantOut    string
		wantErr    string
	}{
		{name: "confirmed", tty: true, input: "y\n", wantRaised: true, wantOut: "Raised BUY-12"},
		{name: "declined", tty: true, input: "n\n", wantOut: "Aborted."},
		{name: "--yes skips the prompt", tty: true, input: "n\n", args: []string{"--yes"}, wantRaised: true, wantOut: "Raised BUY-12"},
		{name: "unattended with -y", args: []string{"-y"}, wantRaised: true, wantOut: "Raised BUY-12"},
		{name: "unattended without --yes", wantErr: "raising a Compute Savings Plan purchase outside a terminal needs --yes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proposeServer(t, purchased(`"plan_type":"compute","created":true`))
			withTerminal(t, tt.tty)
			withStdin(t, tt.input)

			args := append([]string{"commitments", "propose", "--type", "compute"}, tt.args...)
			out, _, err := executeCommand(t, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("propose error: %v", err)
			}
			if raisedIt := got.method != ""; raisedIt != tt.wantRaised {
				t.Errorf("raised = %v, want %v", raisedIt, tt.wantRaised)
			}
			if !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("output missing %q:\n%s", tt.wantOut, out.String())
			}
		})
	}
}

func TestCommitmentsProposeReportsRefusals(t *testing.T) {
	tests := []struct {
		name  string
		reply apiReply
		want  []string
	}{
		{
			name: "another pick is still undecided",
			reply: apiReply{http.StatusConflict, `{"success":false,"error":{"code":"CONFLICT",` +
				`"message":"BUY-12 is already raised for this payer and plan type at $4.100 an hour"}}`},
			want: []string{"BUY-12 is already raised for this payer and plan type"},
		},
		{
			name: "several payers",
			reply: apiReply{http.StatusUnprocessableEntity, `{"success":false,"error":{"code":"VALIDATION_ERROR",` +
				`"message":"This organization has 2 payers. Name one of: 111122223333, 444455556666."}}`},
			want: []string{"Name one of: 111122223333, 444455556666"},
		},
		{
			name: "read-only key",
			reply: apiReply{http.StatusForbidden,
				`{"success":false,"error":{"code":"AUTHORIZATION_ERROR","message":"API key scope insufficient"}}`},
			want: []string{"permission denied", "read-write key"},
		},
		{
			name:  "a reply that is not JSON",
			reply: apiReply{http.StatusCreated, `not json`},
			want:  []string{"invalid JSON response"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proposeServer(t, tt.reply)

			_, _, err := executeCommand(t, "commitments", "propose", "--type", "compute", "--profile", "max_savings", "--yes")
			if err == nil {
				t.Fatal("expected the refusal as an error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), want)
				}
			}
		})
	}
}

func TestCommitmentsProposePrintsTheEnvelopeAsJSON(t *testing.T) {
	proposeServer(t, purchased(`"plan_type":"compute","created":true,"rebindable":true`))

	out, _, err := executeCommand(t, "commitments", "propose", "--type", "compute", "--yes", "--json")
	if err != nil {
		t.Fatalf("propose error: %v", err)
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if envelopeData(envelope)["rebindable"] != true {
		t.Errorf("JSON output lost a field: %s", out.String())
	}
}

func TestCommitmentsProposeUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv(credentialEnvVar, "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "propose", "--type", "compute", "--yes"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
