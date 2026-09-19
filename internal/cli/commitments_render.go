package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

const (
	providerAWS   = "aws"
	providerGCP   = "gcp"
	providerAzure = "azure"

	// notMeasured is printed wherever the API returned no value. It is never
	// abbreviated to a dash or to zero: an unmeasured coverage shown as 0%
	// claims an entirely uncovered fleet, which is a different and alarming
	// statement from "we did not measure this".
	notMeasured = "not measured"

	instrumentRI = "ri"
	instrumentSP = "sp"

	secondsPerDay = 86400
)

// commitmentProviderSupport is the set of providers the commitments routes read
// for. Resolution prefers one of these over the first connected provider,
// because landing a Google Cloud tenant on an empty AWS table reads as a broken
// product rather than as an unsupported one.
var commitmentProviderSupport = map[string]struct{}{
	providerAWS: {},
	providerGCP: {},
}

var providerLabels = map[string]string{
	providerAWS:   "AWS",
	providerGCP:   "Google Cloud",
	providerAzure: "Azure",
}

func providerLabel(id string) string {
	if label, ok := providerLabels[id]; ok {
		return label
	}
	return id
}

func resolveCommitmentProvider(ctx context.Context, client *api.SDKClient, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	resp, err := client.SDK().Providers.List(ctx)
	if err != nil {
		return "", err
	}
	providers := resp.GetData()
	if len(providers) == 0 {
		return "", fmt.Errorf("no providers connected: use --provider to specify one")
	}
	for _, p := range providers {
		if _, ok := commitmentProviderSupport[p.GetProviderID()]; ok {
			return p.GetProviderID(), nil
		}
	}
	return providers[0].GetProviderID(), nil
}

// handleCommitmentsError passes every failure through except the one that is not
// a failure. An API without the commitments routes answers 404, which means "not
// yet", so it is reported as news rather than raised as an error.
func handleCommitmentsError(err error) error {
	if errors.Is(err, api.ErrCommitmentsUnavailable) {
		output.Info("Commitments are not yet available for your account.")
		return nil
	}
	return err
}

// unavailableForProvider reports whether the caller should stop, printing why
// first. Stopping with a sentence beats rendering an empty table, which a
// Google Cloud tenant reads as "you hold no commitments".
func unavailableForProvider(provider, message string) bool {
	if provider == providerAWS {
		return false
	}
	output.Info(fmt.Sprintf(message, providerLabel(provider)))
	return true
}

func pctValue(v float64) string {
	return fmt.Sprintf("%.1f%%", v)
}

func pctOrNotMeasured(v *float64) string {
	if v == nil {
		return notMeasured
	}
	return pctValue(*v)
}

func moneyValue(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func moneyOrNotMeasured(v *float64) string {
	if v == nil {
		return notMeasured
	}
	return moneyValue(*v)
}

func textOrNotMeasured(v *string) string {
	if v == nil || *v == "" {
		return notMeasured
	}
	return *v
}

func boolOrNotMeasured(v *bool) string {
	if v == nil {
		return notMeasured
	}
	if *v {
		return "yes"
	}
	return "no"
}

// expiryCountdown reads the remaining term the API computed rather than
// subtracting dates here, so the terminal and the dashboard count down from the
// same instant.
func expiryCountdown(seconds *int64) string {
	if seconds == nil {
		return notMeasured
	}
	switch days := *seconds / secondsPerDay; {
	case *seconds <= 0:
		return "expired"
	case days == 0:
		return "today"
	default:
		return fmt.Sprintf("%dd", days)
	}
}

// saturationNote names the one reading that looks like success and is not. Full
// utilization with low coverage means every eligible dollar above the commitment
// pays the on-demand rate, so the commitment is undersized rather than optimal.
func saturationNote(saturated bool) string {
	if saturated {
		return "saturated"
	}
	return ""
}

// parseWindow accepts d, w and m suffixes. A month is 30 days here, stated
// rather than approximated silently, because an expiry window is a question
// about roughly how far ahead to look.
func parseWindow(value string) (time.Duration, error) {
	if len(value) < 2 {
		return 0, invalidWindow(value)
	}
	unit, ok := windowUnits[value[len(value)-1:]]
	if !ok {
		return 0, invalidWindow(value)
	}
	count, err := strconv.Atoi(value[:len(value)-1])
	if err != nil || count < 0 {
		return 0, invalidWindow(value)
	}
	return time.Duration(count) * unit, nil
}

func invalidWindow(value string) error {
	return fmt.Errorf("invalid window %q: use a number followed by d, w or m, for example 30d", value)
}

var windowUnits = map[string]time.Duration{
	"d": 24 * time.Hour,
	"w": 7 * 24 * time.Hour,
	"m": 30 * 24 * time.Hour,
}

// withinWindow keeps rows whose remaining term fits the window. A row with no
// measured expiry is dropped rather than kept, because an expiry report that
// lists commitments it cannot date is not a report.
func withinWindow(rows []api.CommitmentPortfolioRow, window time.Duration) []api.CommitmentPortfolioRow {
	limit := int64(window / time.Second)
	kept := make([]api.CommitmentPortfolioRow, 0, len(rows))
	for _, row := range rows {
		if row.ExpiresInSeconds != nil && *row.ExpiresInSeconds <= limit {
			kept = append(kept, row)
		}
	}
	return kept
}

func commitmentsWebPath(provider, instrument, commitmentID string) string {
	params := map[string]string{"section": instrument, "provider": provider, "commitment": commitmentID}
	return "/commitments" + api.BuildQueryString(params)
}
