package mcp

import "fmt"

// The two places the REST payload and the hosted tool payload genuinely differ.
// Both are ports, not inventions: they mirror the hosted server's own slimming.

// detailActionDuplicates are returned at the top level of a recommendation
// already, so echoing them inside actions ships the same payload twice.
var detailActionDuplicates = map[string]bool{
	"metrics": true, "comparison_data": true, "risk_assessment": true, "implementation_steps": true,
}

// detailOperatorFields are operator artifacts. Applying happens in the dashboard
// or the CLI, so an agent has no use for them and they are the largest fields in
// the payload. Dropping iam_policy also keeps a permissions document out of a
// model's context, which is the reason it is dropped on the hosted side too.
var detailOperatorFields = map[string]bool{
	"iam_policy": true, "iam_role_arn": true, "manual_instructions": true,
}

const maxMetricPoints = 60

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
	if metrics, ok := slim["metrics"].([]any); ok {
		capped := make([]any, 0, len(metrics))
		for _, series := range metrics {
			capped = append(capped, capSeries(series))
		}
		slim["metrics"] = capped
	}
	return slim
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

// shapeWhoami maps GET /api/v1/auth/whoami onto the field names the hosted
// whoami tool returns. The REST route builds its accounts list straight from the
// tenant's enabled providers (one entry per provider, no account id), so those
// entries are the provider list under a name that would mislead a model here.
// Evaluation counters are not included: they come from a route this credential
// cannot read, so they are absent rather than guessed, and get-identity's
// description does not promise them.
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
