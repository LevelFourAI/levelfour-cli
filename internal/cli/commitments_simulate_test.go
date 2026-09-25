package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	kr "github.com/zalando/go-keyring"
)

const simulationPath = api.CommitmentsPath + "/purchase-simulation"

type simulateRequest struct {
	method string
	path   string
	query  url.Values
}

func simulateServer(t *testing.T, reply apiReply) *simulateRequest {
	t.Helper()
	got := &simulateRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.query = r.Method, r.URL.Path, r.URL.Query()
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	}))
	t.Cleanup(srv.Close)
	useCommitmentsServer(t, srv)
	return got
}

func simulation(fields string) apiReply {
	return apiReply{status: http.StatusOK, body: `{"success":true,"data":{"payer_account_id":"111122223333",` +
		`"plan_type":"compute","term_months":12,"payment_option":"no_upfront",` +
		`"candidate":{"commitment_hourly":12.5},` + fields + `}}`}
}

const (
	replayedWindow = `"window":{"first_day":"2026-07-22","last_day":"2026-09-19","days":59,"missing_days":1}`

	replayedTotals = `"totals":{"eligible_on_demand":90000,"covered_on_demand":52000,"uncovered_on_demand":38000,` +
		`"commitment_cost":18000,"used_commitment":17500,"wasted_commitment":500,"utilization_pct":97.2,` +
		`"coverage_pct":57.8,"net_savings_monthly":16800,"net_savings_monthly_at_your_rates":15100}`

	dailyCaveat = `"caveats":["Sized on daily totals. A day's average hides its quietest hours, so utilization ` +
		`and savings here are upper bounds."]`
)

func replayed() apiReply {
	return simulation(replayedWindow + "," + replayedTotals + "," + dailyCaveat + `,"profiles":` + sizedProfiles +
		`,"aws":{"hourly_commitment_to_purchase":14.2,"cap":13.9},"series":[{"day":"2026-07-22"}]`)
}

func TestCommitmentsSimulateReplaysTheCommitment(t *testing.T) {
	got := simulateServer(t, replayed())

	out, _, err := executeCommand(t, "commitments", "simulate", "--type", "compute", "--commitment", "12.5")
	if err != nil {
		t.Fatalf("simulate error: %v", err)
	}
	if got.method != http.MethodGet || got.path != simulationPath {
		t.Errorf("request = %s %s, want GET %s", got.method, got.path, simulationPath)
	}
	wantQuery := url.Values{"plan_type": {"compute"}, "commitment_hourly": {"12.5"}, "term_months": {"12"},
		"payment_option": {"no_upfront"}, "lookback_days": {"60"}}
	if got.query.Encode() != wantQuery.Encode() {
		t.Errorf("query = %s, want %s", got.query.Encode(), wantQuery.Encode())
	}
	for _, want := range []string{"Compute Savings Plan at $12.500/hr, 12 months, No Upfront", "111122223333",
		"2026-07-22 to 2026-09-19, 59 days (1 missing)", "97.2%", "57.8%", "$16800.00/mo", "$15100.00/mo",
		"$90000.00", "$52000.00", "$38000.00", "$18000.00", "$17500.00", "$500.00", "Sized profiles",
		"Max savings", "$4.100", "$14.200/hr", "$13.900/hr", "upper bounds"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "(recommended)") {
		t.Errorf("a simulation recommends no profile:\n%s", out.String())
	}
}

func TestCommitmentsSimulateSendsEveryFlag(t *testing.T) {
	got := simulateServer(t, replayed())

	_, _, err := executeCommand(t, "commitments", "simulate", "--type", "database", "--commitment", "0.75",
		"--term", "3y", "--payment", "all_upfront", "--payer", "111122223333", "--lookback", "30")
	if err != nil {
		t.Fatalf("simulate error: %v", err)
	}
	wantQuery := url.Values{"plan_type": {"database"}, "commitment_hourly": {"0.75"}, "term_months": {"36"},
		"payment_option": {"all_upfront"}, "payer_account_id": {"111122223333"}, "lookback_days": {"30"}}
	if got.query.Encode() != wantQuery.Encode() {
		t.Errorf("query = %s, want %s", got.query.Encode(), wantQuery.Encode())
	}
}

func TestCommitmentsSimulateLeavesOutAnAWSFigureItDoesNotHave(t *testing.T) {
	simulateServer(t, simulation(replayedWindow+","+replayedTotals+`,"profiles":`+sizedProfiles+`,"aws":null`))

	out, _, err := executeCommand(t, "commitments", "simulate", "--type", "compute", "--commitment", "12.5")
	if err != nil {
		t.Fatalf("simulate error: %v", err)
	}
	if strings.Contains(out.String(), "AWS recommends") {
		t.Errorf("no AWS figure should print no AWS line:\n%s", out.String())
	}
}

func TestCommitmentsSimulateSaysWhyNothingWasReplayed(t *testing.T) {
	tests := []struct {
		name   string
		fields string
		want   []string
	}{
		{
			name:   "too little history",
			fields: replayedWindow + `,"unavailable_reason":"insufficient_history","totals":null,"profiles":null`,
			want:   []string{"59 days (1 missing)", "Nothing to replay: too few days of billing data yet."},
		},
		{
			name:   "no daily billing data",
			fields: `"window":null,"unavailable_reason":"no_cur","totals":null`,
			want: []string{"Window: " + notMeasured,
				"Nothing to replay: no daily billing data is loaded for this payer."},
		},
		{
			name:   "no reason given",
			fields: `"payer_account_id":null,"unavailable_reason":null,"totals":null`,
			want:   []string{"Payer: " + notMeasured, "Nothing to replay."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			simulateServer(t, simulation(tt.fields))

			out, _, err := executeCommand(t, "commitments", "simulate", "--type", "compute", "--commitment", "12.5")
			if err != nil {
				t.Fatalf("simulate error: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q:\n%s", want, out.String())
				}
			}
			if strings.Contains(out.String(), "Sized profiles") {
				t.Errorf("nothing replayed should print no profiles:\n%s", out.String())
			}
		})
	}
}

func TestCommitmentsSimulateRefusesFlagsTheAPIWouldRefuse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no type", []string{"--commitment", "12.5"}, "--type is required: choose one of compute, database"},
		{"unknown type", []string{"--type", "gpu", "--commitment", "12.5"}, `invalid --type "gpu"`},
		{"no commitment", []string{"--type", "compute"}, "--commitment is required"},
		{"zero commitment", []string{"--type", "compute", "--commitment", "0"}, "invalid --commitment 0: it must be above zero"},
		{"negative commitment", []string{"--type", "compute", "--commitment", "-1.5"}, "invalid --commitment -1.5"},
		{"unknown term", []string{"--type", "compute", "--commitment", "1", "--term", "2y"}, `invalid --term "2y"`},
		{"unknown payment", []string{"--type", "compute", "--commitment", "1", "--payment", "monthly"}, `invalid --payment "monthly"`},
		{"unknown lookback", []string{"--type", "compute", "--commitment", "1", "--lookback", "90"}, `invalid --lookback "90"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := simulateServer(t, replayed())

			_, _, err := executeCommand(t, append([]string{"commitments", "simulate"}, tt.args...)...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if got.method != "" {
				t.Error("a refused flag must not reach the API")
			}
		})
	}
}

func TestCommitmentsSimulateReportsRefusals(t *testing.T) {
	tests := []struct {
		name  string
		reply apiReply
		want  string
	}{
		{
			name: "unknown payer",
			reply: apiReply{http.StatusNotFound, `{"success":false,"error":{"code":"NOT_FOUND",` +
				`"message":"No payer 999999999999 bills this organization. Its payers: 111122223333."}}`},
			want: "No payer 999999999999 bills this organization",
		},
		{
			name: "a term a Database plan is not sold for",
			reply: apiReply{http.StatusUnprocessableEntity, `{"success":false,"error":{"code":"VALIDATION_ERROR",` +
				`"message":"A Database Savings Plan is sold for 12 months, No Upfront, only."}}`},
			want: "sold for 12 months, No Upfront, only",
		},
		{
			name:  "a reply that is not JSON",
			reply: apiReply{http.StatusOK, `not json`},
			want:  "invalid JSON response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			simulateServer(t, tt.reply)

			_, _, err := executeCommand(t, "commitments", "simulate", "--type", "database", "--commitment", "2",
				"--term", "3y")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestCommitmentsSimulatePrintsTheEnvelopeAsJSON(t *testing.T) {
	simulateServer(t, replayed())

	out, _, err := executeCommand(t, "commitments", "simulate", "--type", "compute", "--commitment", "12.5", "--json")
	if err != nil {
		t.Fatalf("simulate error: %v", err)
	}
	var envelope map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, out.String())
	}
	if _, ok := envelopeData(envelope)["series"]; !ok {
		t.Errorf("JSON output lost a field: %s", out.String())
	}
}

func TestCommitmentsSimulateUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv(credentialEnvVar, "")
	defer resetFlags()

	if _, _, err := executeCommand(t, "commitments", "simulate", "--type", "compute", "--commitment", "1"); err == nil {
		t.Error("expected an error when not authenticated")
	}
}
