package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

func TestParseWindow(t *testing.T) {
	cases := []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "30d", want: 30 * 24 * time.Hour},
		{value: "12w", want: 84 * 24 * time.Hour},
		{value: "6m", want: 180 * 24 * time.Hour},
		{value: "0d", want: 0},
		{value: "", wantErr: true},
		{value: "d", wantErr: true},
		{value: "30", wantErr: true},
		{value: "30y", wantErr: true},
		{value: "-5d", wantErr: true},
		{value: "manyd", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseWindow(tc.value)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseWindow(%q) = %v, want an error", tc.value, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseWindow(%q) error: %v", tc.value, err)
			}
			if got != tc.want {
				t.Errorf("parseWindow(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestExpiryCountdown(t *testing.T) {
	past, today, weeks := int64(-60), int64(3600), int64(1555200)
	cases := []struct {
		name    string
		seconds *int64
		want    string
	}{
		{"unmeasured", nil, notMeasured},
		{"past", &past, "expired"},
		{"inside a day", &today, "today"},
		{"days away", &weeks, "18d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := expiryCountdown(tc.seconds); got != tc.want {
				t.Errorf("expiryCountdown() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSaturationNoteOnlyNamesTheMisleadingCase(t *testing.T) {
	if got := saturationNote(true); got != "saturated" {
		t.Errorf("saturationNote(true) = %q", got)
	}
	if got := saturationNote(false); got != "" {
		t.Errorf("saturationNote(false) = %q, want empty", got)
	}
}

func TestNotMeasuredFormatters(t *testing.T) {
	pct, money := 61.4, 1033.33
	text, empty := "all_upfront", ""
	yes, no := true, false

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"percent absent", pctOrNotMeasured(nil), notMeasured},
		{"percent present", pctOrNotMeasured(&pct), "61.4%"},
		{"money absent", moneyOrNotMeasured(nil), notMeasured},
		{"money present", moneyOrNotMeasured(&money), "$1033.33"},
		{"text absent", textOrNotMeasured(nil), notMeasured},
		{"text empty", textOrNotMeasured(&empty), notMeasured},
		{"text present", textOrNotMeasured(&text), "all_upfront"},
		{"bool absent", boolOrNotMeasured(nil), notMeasured},
		{"bool true", boolOrNotMeasured(&yes), "yes"},
		{"bool false", boolOrNotMeasured(&no), "no"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestHandleCommitmentsErrorPassesRealFailuresThrough(t *testing.T) {
	captureOutput(t)

	boom := errors.New("boom")
	if err := handleCommitmentsError(boom); !errors.Is(err, boom) {
		t.Errorf("handleCommitmentsError() = %v, want the original error", err)
	}
	if err := handleCommitmentsError(api.ErrCommitmentsUnavailable); err != nil {
		t.Errorf("a missing surface should not be an error, got %v", err)
	}
}

func TestCommitmentsWebPathDropsEmptyKeys(t *testing.T) {
	path := commitmentsWebPath(providerAWS, instrumentRI, "")
	if strings.Contains(path, "commitment=") {
		t.Errorf("path = %q, should omit an empty commitment", path)
	}
	if !strings.HasPrefix(path, "/commitments?") {
		t.Errorf("path = %q, want the commitments page", path)
	}

	path = commitmentsWebPath(providerGCP, instrumentSP, "cud-1")
	for _, want := range []string{"section=sp", "provider=gcp", "commitment=cud-1"} {
		if !strings.Contains(path, want) {
			t.Errorf("path = %q, missing %q", path, want)
		}
	}
}

func TestDimensionSummaryNamesAnUnmeasuredDimension(t *testing.T) {
	if got := dimensionSummary(nil); got != notMeasured {
		t.Errorf("dimensionSummary(nil) = %q, want %q", got, notMeasured)
	}
	got := dimensionSummary([]api.UtilizationDimension{
		{Used: 1240, Total: 1263, Unit: "vCPU"},
		{Used: 12, Total: 16, Unit: "nodes"},
	})
	if got != "1240/1263 vCPU, 12/16 nodes" {
		t.Errorf("dimensionSummary() = %q", got)
	}
}

func TestOverageCellSaysWhatExceedingTheFloorMeans(t *testing.T) {
	if got := overageCell(true); !strings.Contains(got, "billing above") {
		t.Errorf("overageCell(true) = %q", got)
	}
	if got := overageCell(false); got != "no" {
		t.Errorf("overageCell(false) = %q", got)
	}
}

func TestPortfolioFeeFollowsTheBasis(t *testing.T) {
	totals := api.CommitmentPortfolioTotals{FeeMonthlyList: 900, FeeMonthlyNet: 800}
	if got := portfolioFee(totals, "list"); got != 900 {
		t.Errorf("portfolioFee(list) = %v, want 900", got)
	}
	if got := portfolioFee(totals, "net"); got != 800 {
		t.Errorf("portfolioFee(net) = %v, want 800", got)
	}
}
