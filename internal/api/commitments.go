package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// CommitmentsPath is the base path every commitments route hangs off.
const CommitmentsPath = "/api/v1/commitments"

// ErrCommitmentsUnavailable reports a 404 from a commitments route. The routes
// reach an API that serves them and one that does not, so a caller distinguishes
// "this deployment has no commitments surface" from a request that failed.
var ErrCommitmentsUnavailable = errors.New("commitments are not available on this API")

// A nil pointer on any field below means the API did not measure that value.
// It is never the same as zero: an unmeasured coverage rendered as 0% draws an
// entirely uncovered fleet, which is a different and alarming claim.

type CommitmentAccountEsr struct {
	AccountID           string  `json:"account_id"`
	AccountName         string  `json:"account_name"`
	MonthlySpend        float64 `json:"monthly_spend"`
	EsrPct              float64 `json:"esr_pct"`
	MonthlySaved        float64 `json:"monthly_saved"`
	CouldSaveAdditional float64 `json:"could_save_additional"`
}

type CommitmentsOverview struct {
	GlobalEsr                         float64                `json:"global_esr"`
	BenchmarkEsr                      float64                `json:"benchmark_esr"`
	TotalCommittedMonthly             float64                `json:"total_committed_monthly"`
	TotalCloudSpendMonthly            float64                `json:"total_cloud_spend_monthly"`
	CoveragePct                       float64                `json:"coverage_pct"`
	CoverageDeltaPp                   float64                `json:"coverage_delta_pp"`
	EstimatedWasteMonthly             float64                `json:"estimated_waste_monthly"`
	WasteCommitmentCount              int                    `json:"waste_commitment_count"`
	PotentialAdditionalSavingsMonthly float64                `json:"potential_additional_savings_monthly"`
	CommitmentCount                   int                    `json:"commitment_count"`
	ExpiringCount                     int                    `json:"expiring_count"`
	Accounts                          []CommitmentAccountEsr `json:"accounts"`
}

type CommitmentInstrumentBreakdown struct {
	UtilizationPct   float64 `json:"utilization_pct"`
	CoveragePct      float64 `json:"coverage_pct"`
	UnusedMonthly    float64 `json:"unused_monthly"`
	UncoveredMonthly float64 `json:"uncovered_monthly"`
	CommitmentCount  int     `json:"commitment_count"`
}

type CommitmentServiceRow struct {
	Service      string                        `json:"service"`
	ServiceLabel string                        `json:"service_label"`
	RI           CommitmentInstrumentBreakdown `json:"ri"`
	SP           CommitmentInstrumentBreakdown `json:"sp"`
}

type CommitmentsByService struct {
	Provider string                 `json:"provider"`
	Services []CommitmentServiceRow `json:"services"`
}

// EsrRates carries Measured because the three rates are null when no on-demand
// equivalent was exported for the scope, which is not a rate of zero.
type EsrRates struct {
	Measured           bool     `json:"measured"`
	OnDemandEquivalent float64  `json:"on_demand_equivalent"`
	EffectiveCost      float64  `json:"effective_cost"`
	CommitmentSaved    float64  `json:"commitment_saved"`
	NegotiatedSaved    float64  `json:"negotiated_saved"`
	TotalSaved         float64  `json:"total_saved"`
	CommitmentRatePct  *float64 `json:"commitment_rate_pct"`
	NegotiatedRatePct  *float64 `json:"negotiated_rate_pct"`
	TotalRatePct       *float64 `json:"total_rate_pct"`
}

type EsrAccount struct {
	EsrRates
	AccountID   string `json:"account_id"`
	AccountName string `json:"account_name"`
}

type CommitmentEsr struct {
	Period           *string      `json:"period"`
	Scope            string       `json:"scope"`
	MeasuredSharePct float64      `json:"measured_share_pct"`
	Totals           *EsrRates    `json:"totals"`
	Accounts         []EsrAccount `json:"accounts"`
}

type CommitmentConsumer struct {
	AccountID           string  `json:"account_id"`
	AccountName         string  `json:"account_name"`
	CoveredSpendMonthly float64 `json:"covered_spend_monthly"`
	CoveredHours        float64 `json:"covered_hours"`
}

type CommitmentPortfolioRow struct {
	ID                    string               `json:"id"`
	Service               string               `json:"service"`
	ServiceLabel          string               `json:"service_label"`
	Kind                  string               `json:"kind"`
	HolderAccountID       string               `json:"holder_account_id"`
	HolderAccountName     string               `json:"holder_account_name"`
	Consumers             []CommitmentConsumer `json:"consumers"`
	OrganizationID        string               `json:"organization_id"`
	Region                string               `json:"region"`
	AppliesTo             *string              `json:"applies_to"`
	UnitCount             int                  `json:"unit_count"`
	UnitRateHourly        *float64             `json:"unit_rate_hourly"`
	HourlyCommitmentUSD   *float64             `json:"hourly_commitment_usd"`
	FeeMonthlyList        *float64             `json:"fee_monthly_list"`
	FeeMonthlyNet         *float64             `json:"fee_monthly_net"`
	EndAt                 *string              `json:"end_at"`
	ExpiresInSeconds      *int64               `json:"expires_in_seconds"`
	UtilizationPct        float64              `json:"utilization_pct"`
	CoveragePct           float64              `json:"coverage_pct"`
	UtilizationSlopePp30d *float64             `json:"utilization_slope_pp_30d"`
	Saturated             bool                 `json:"saturated"`
	ProtectsMonthly       *float64             `json:"protects_monthly"`
	RightsizingMonthly    *float64             `json:"rightsizing_monthly"`
	Exchangeable          *bool                `json:"exchangeable"`
	Cancellable           *bool                `json:"cancellable"`
	PredecessorID         *string              `json:"predecessor_id"`
	Status                string               `json:"status"`
}

// CommitmentPortfolioTotals is the API's own arithmetic. Nothing here is
// re-derived by summing a column, because monthly_commitment_usd reads 0.0 on
// some No Upfront terms while a sibling commitment reads a real figure, and a
// client-side total would quietly swallow the gap.
type CommitmentPortfolioTotals struct {
	CommitmentCount     int     `json:"commitment_count"`
	HourlyCommitmentUSD float64 `json:"hourly_commitment_usd"`
	FeeMonthlyList      float64 `json:"fee_monthly_list"`
	FeeMonthlyNet       float64 `json:"fee_monthly_net"`
	ProtectsMonthly     float64 `json:"protects_monthly"`
	RightsizingMonthly  float64 `json:"rightsizing_monthly"`
	ExpiringWithin30d   int     `json:"expiring_within_30d"`
	OrganizationCount   int     `json:"organization_count"`
}

type CommitmentPortfolio struct {
	Basis  string                    `json:"basis"`
	Totals CommitmentPortfolioTotals `json:"totals"`
	Rows   []CommitmentPortfolioRow  `json:"rows"`
}

type CommitmentListItem struct {
	ID                    string  `json:"id"`
	Provider              string  `json:"provider"`
	Service               string  `json:"service"`
	ServiceLabel          string  `json:"service_label"`
	AccountID             string  `json:"account_id"`
	AccountName           string  `json:"account_name"`
	Region                string  `json:"region"`
	Kind                  string  `json:"kind"`
	StartDate             string  `json:"start_date"`
	EndDate               string  `json:"end_date"`
	Status                string  `json:"status"`
	CurrentUtilizationPct float64 `json:"current_utilization_pct"`
	CurrentCoveragePct    float64 `json:"current_coverage_pct"`
	MonthlyCommitmentUSD  float64 `json:"monthly_commitment_usd"`
}

type CommitmentListPagination struct {
	TotalItems  int  `json:"total_items"`
	TotalPages  int  `json:"total_pages"`
	CurrentPage int  `json:"current_page"`
	PageSize    int  `json:"page_size"`
	HasNext     bool `json:"has_next"`
	HasPrevious bool `json:"has_previous"`
}

type CommitmentList struct {
	Items      []CommitmentListItem     `json:"items"`
	Pagination CommitmentListPagination `json:"pagination"`
}

type CommitmentDetailRecommendation struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	MonthlySavings float64 `json:"monthly_savings"`
	Confidence     string  `json:"confidence"`
	Summary        string  `json:"summary"`
}

type CommitmentDetail struct {
	CommitmentListItem
	TermMonths          int                              `json:"term_months"`
	PaymentOption       *string                          `json:"payment_option"`
	HourlyCommitmentUSD *float64                         `json:"hourly_commitment_usd"`
	IsCommitable        bool                             `json:"is_commitable"`
	EndAt               *string                          `json:"end_at"`
	HolderAccountID     *string                          `json:"holder_account_id"`
	UnitCount           int                              `json:"unit_count"`
	FeeMonthlyList      *float64                         `json:"fee_monthly_list"`
	FeeMonthlyNet       *float64                         `json:"fee_monthly_net"`
	ProtectsMonthly     *float64                         `json:"protects_monthly"`
	RightsizingMonthly  *float64                         `json:"rightsizing_monthly"`
	Exchangeable        *bool                            `json:"exchangeable"`
	Cancellable         *bool                            `json:"cancellable"`
	Consumers           []CommitmentConsumer             `json:"consumers"`
	Recommendations     []CommitmentDetailRecommendation `json:"recommendations"`
}

type RenewalPendingChange struct {
	RecommendationID string  `json:"recommendation_id"`
	Service          string  `json:"service"`
	Account          string  `json:"account"`
	MonthlySavings   float64 `json:"monthly_savings"`
	Status           string  `json:"status"`
}

type CommitmentRenewalPlan struct {
	CommitmentID       string                 `json:"commitment_id"`
	Service            string                 `json:"service"`
	HolderAccountID    string                 `json:"holder_account_id"`
	EndAt              *string                `json:"end_at"`
	BuyAfterUTC        *string                `json:"buy_after_utc"`
	ExpiresInSeconds   *int64                 `json:"expires_in_seconds"`
	UnitsHeld          int                    `json:"units_held"`
	UnitsConsumed      int                    `json:"units_consumed"`
	UnitsRecommended   int                    `json:"units_recommended"`
	ProtectsMonthly    float64                `json:"protects_monthly"`
	RightsizingMonthly float64                `json:"rightsizing_monthly"`
	PendingChanges     []RenewalPendingChange `json:"pending_changes"`
	Exchangeable       *bool                  `json:"exchangeable"`
	Cancellable        *bool                  `json:"cancellable"`
}

type UtilizationDimension struct {
	Key            string  `json:"key"`
	Used           float64 `json:"used"`
	Total          float64 `json:"total"`
	Unit           string  `json:"unit"`
	UtilizationPct float64 `json:"utilization_pct"`
}

type CommitmentApplication struct {
	Service   string  `json:"service"`
	HourlyUSD float64 `json:"hourly_usd"`
}

// CommitmentUtilizationDay covers both instruments, and each fills the half that
// applies to it. A plan day carries no coverage at all: Cost Explorer cannot
// filter coverage by plan type, so what one plan covers is unanswerable.
type CommitmentUtilizationDay struct {
	Date               string   `json:"date"`
	CapacityPct        *float64 `json:"capacity_pct"`
	CapacityUsed       *float64 `json:"capacity_used"`
	CapacityTotal      *float64 `json:"capacity_total"`
	CommitmentPct      *float64 `json:"commitment_pct"`
	CommitmentUsedUSD  *float64 `json:"commitment_used_usd"`
	CommitmentTotalUSD *float64 `json:"commitment_total_usd"`
	CoveragePct        *float64 `json:"coverage_pct"`
}

type ServiceUtilization struct {
	Service             string                     `json:"service"`
	ServiceLabel        string                     `json:"service_label"`
	UtilizationPct      float64                    `json:"utilization_pct"`
	Dimensions          []UtilizationDimension     `json:"dimensions"`
	CommittedMonthlyUSD *float64                   `json:"committed_monthly_usd"`
	CommittedHourlyUSD  *float64                   `json:"committed_hourly_usd"`
	ContractedHourlyUSD *float64                   `json:"contracted_hourly_usd"`
	AppliedTo           []CommitmentApplication    `json:"applied_to"`
	Days                []CommitmentUtilizationDay `json:"days"`
}

type CommitmentUtilization struct {
	Instrument string               `json:"instrument"`
	Services   []ServiceUtilization `json:"services"`
}

type UncoveredSlice struct {
	Service                   string  `json:"service"`
	OrganizationID            string  `json:"organization_id"`
	Platform                  string  `json:"platform"`
	OnDemandHourlyAvg         float64 `json:"on_demand_hourly_avg"`
	OnDemandHourlyMin         float64 `json:"on_demand_hourly_min"`
	VolatilityRatio           float64 `json:"volatility_ratio"`
	RecommendedKind           *string `json:"recommended_kind"`
	SuggestedCommitmentHourly float64 `json:"suggested_commitment_hourly"`
}

type CommitmentRecommendation struct {
	ID             string  `json:"id"`
	Kind           string  `json:"kind"`
	Title          string  `json:"title"`
	Detail         string  `json:"detail"`
	MonthlySavings float64 `json:"monthly_savings"`
	Confidence     string  `json:"confidence"`
}

type ContractMonth struct {
	Period      string  `json:"period"`
	ContractUSD float64 `json:"contract_usd"`
	MeteredUSD  float64 `json:"metered_usd"`
}

type CommitmentContract struct {
	ID                  string          `json:"id"`
	Vendor              string          `json:"vendor"`
	ContractMonthlyUSD  *float64        `json:"contract_monthly_usd"`
	LatestMeteredUSD    float64         `json:"latest_metered_usd"`
	OverageExceedsFloor bool            `json:"overage_exceeds_floor"`
	Months              []ContractMonth `json:"months"`
}

// Dimension is a service on a reservation and a plan type on a plan.
type CoverageRateService struct {
	Instrument  string  `json:"instrument"`
	Dimension   string  `json:"dimension"`
	CoveragePct float64 `json:"coverage_pct"`
	MeasuredOn  *string `json:"measured_on"`
}

type CoverageRateAccount struct {
	AccountID      string  `json:"account_id"`
	AccountName    string  `json:"account_name"`
	CoveredMonthly float64 `json:"covered_monthly"`
}

type CoverageRateUncovered struct {
	Service         string  `json:"service"`
	OnDemandMonthly float64 `json:"on_demand_monthly"`
}

// CoveredMonthly is nil on Google Cloud, which stores only the commitment fee.
// Reporting that as covered spend would exceed the bill it claims to cover.
type CoverageRateTotals struct {
	CoveredMonthly   *float64 `json:"covered_monthly"`
	UncoveredMonthly float64  `json:"uncovered_monthly"`
	CoveragePct      *float64 `json:"coverage_pct"`
}

// Measured separates a sweep that has not run from a provider covering nothing.
type CommitmentCoverage struct {
	Provider           string                  `json:"provider"`
	Measured           bool                    `json:"measured"`
	Services           []CoverageRateService   `json:"services"`
	Accounts           []CoverageRateAccount   `json:"accounts"`
	UncoveredByService []CoverageRateUncovered `json:"uncovered_by_service"`
	Totals             *CoverageRateTotals     `json:"totals"`
}

type commitmentsEnvelope[T any] struct {
	Data T `json:"data"`
}

// GetCommitments reads one commitments route and returns both the decoded
// payload and the untouched response body. Callers render from the first and
// hand the second to --json, --jq and --template, so a formatting flag shows
// every field the API sent rather than only the ones modelled here.
func GetCommitments[T any](c *RawClient, route string, params map[string]string) (T, json.RawMessage, error) {
	var envelope commitmentsEnvelope[T]

	raw, err := c.DoRaw(http.MethodGet, CommitmentsPath+route+BuildQueryString(params), nil)
	if err != nil {
		return envelope.Data, nil, err
	}
	if raw.StatusCode == http.StatusNotFound {
		return envelope.Data, nil, ErrCommitmentsUnavailable
	}
	if raw.StatusCode >= 400 {
		return envelope.Data, nil, raw.DecodeError()
	}
	if err := json.Unmarshal(raw.Body, &envelope); err != nil {
		return envelope.Data, nil, fmt.Errorf("unexpected response from %s: %w", CommitmentsPath+route, err)
	}
	return envelope.Data, raw.Body, nil
}
