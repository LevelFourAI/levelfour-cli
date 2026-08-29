package mcp

import "fmt"

// The two places the REST payload and the hosted tool payload genuinely differ.

// detailActionDuplicates are returned at the top level of a recommendation
// already, so echoing them inside actions ships the same payload twice.
var detailActionDuplicates = map[string]bool{
	"metrics": true, "comparison_data": true, "risk_assessment": true, "implementation_steps": true,
}

// Operator artifacts: applying happens in the dashboard or the CLI, so an agent
// has no use for them and they are the largest fields in the payload.
var detailOperatorFields = map[string]bool{
	"iam_policy": true, "iam_role_arn": true, "manual_instructions": true,
}

const (
	metricsKey = "metrics"

	// The number of series before any capping. A later size trim reports against
	// this, so a model is never given a denominator that counts only survivors.
	metricsTotalKey = "metrics_total"
)

const (
	maxMetricPoints = 60

	// A fleet-sized recommendation carries one series per affected resource, and
	// metrics sit outside the field fitBudget trims, so nothing else bounds them.
	maxMetricSeries = 12
)

func slimRecommendation(detail map[string]any) map[string]any {
	slim := map[string]any{}
	for k, v := range detail {
		if !detailOperatorFields[k] {
			slim[k] = v
		}
	}
	if actions, ok := slim["actions"].(map[string]any); ok {
		trimmed := map[string]any{}
		for k, v := range actions {
			if !detailActionDuplicates[k] && !detailOperatorFields[k] {
				trimmed[k] = v
			}
		}
		slim["actions"] = trimmed
	}
	if metrics, ok := slim[metricsKey].([]any); ok {
		kept := capMetrics(metrics)
		slim[metricsKey] = kept
		if len(metrics) > len(kept) {
			slim[metricsTotalKey] = len(metrics)
		}
	}
	return slim
}

func capMetrics(metrics []any) []any {
	kept := metrics
	if len(kept) > maxMetricSeries {
		kept = kept[:maxMetricSeries]
	}
	capped := make([]any, 0, len(kept))
	for _, series := range kept {
		capped = append(capped, capSeries(series))
	}
	return capped
}

func capSeries(series any) any {
	asMap, ok := series.(map[string]any)
	if !ok {
		return series
	}
	points, ok := asMap["data_points"].([]any)
	if !ok || len(points) <= maxMetricPoints {
		return series
	}
	capped := map[string]any{}
	for k, v := range asMap {
		capped[k] = v
	}
	capped["data_points"] = points[len(points)-maxMetricPoints:]
	capped["truncated"] = fmt.Sprintf("Showing the most recent %d of %d points.", maxMetricPoints, len(points))
	return capped
}

// The REST route's "accounts" list is one entry per enabled provider with no
// account id, so it is renamed rather than passed through under a name that
// would mislead a model. Evaluation counters come from a route this credential
// cannot read, so they are absent rather than guessed.
func shapeWhoami(payload map[string]any) map[string]any {
	providers := []any{}
	if accounts, ok := payload["accounts"].([]any); ok {
		for _, entry := range accounts {
			if account, ok := entry.(map[string]any); ok {
				if name, ok := account["provider"].(string); ok && name != "" {
					providers = append(providers, name)
				}
			}
		}
	}
	return map[string]any{
		"organization":      payload["organization"],
		"plan":              payload["plan"],
		"role":              payload["role"],
		"enabled_providers": providers,
		"served_by":         "l4 mcp serve (local stdio), backed by the LevelFour REST API",
	}
}
