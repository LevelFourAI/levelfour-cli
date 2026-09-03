package cli

import "strings"

// Status tokens as the LevelFour API returns them.
const (
	statusAvailable        = "available"
	statusPending          = "pending"
	statusAwaitingApproval = "awaiting_approval"
	statusProcessing       = "processing"
	statusInProgress       = "in_progress"
	statusCompleted        = "completed"
	statusFailed           = "failed"
	statusWarning          = "warning"
	statusOptimized        = "optimized"
	statusSaved            = "saved"
	statusRejected         = "rejected"
	statusUnavailable      = "unavailable"
)

const (
	savingAcceptanceAccepted        = "accepted"
	executionRequestPendingApproval = "pending_approval"
)

const (
	labelAvailable     = "Available"
	labelPending       = "Pending"
	labelNeedsApproval = "Needs Approval"
	labelProcessing    = "Processing"
	labelInProgress    = "In Progress"
	labelCompleted     = "Completed"
	labelFailed        = "Failed"
	labelWarning       = "Warning"
	labelSaved         = "Saved"
	labelRejected      = "Rejected"
	labelUnavailable   = "Unavailable"
)

var recommendationStatusLabels = map[string]string{
	statusAvailable:        labelAvailable,
	statusPending:          labelPending,
	statusAwaitingApproval: labelNeedsApproval,
	statusProcessing:       labelProcessing,
	statusInProgress:       labelInProgress,
	statusCompleted:        labelCompleted,
	statusFailed:           labelFailed,
	statusWarning:          labelWarning,
	statusOptimized:        labelSaved,
	statusSaved:            labelSaved,
	statusRejected:         labelRejected,
	statusUnavailable:      labelUnavailable,
}

// recommendationStatusLabel renders a status token as a person reads it. An unknown value passes
// through untouched, so a status added server-side degrades to its raw form rather than vanishing.
func recommendationStatusLabel(status string) string {
	if label, ok := recommendationStatusLabels[strings.ToLower(status)]; ok {
		return label
	}
	return status
}

// recommendationStatusFromItem renders the status of a list row or a recommendation detail.
func recommendationStatusFromItem(raw string, extra map[string]interface{}) string {
	return recommendationStatusLabel(recommendationDisplayStatusFromExtra(raw, extra))
}

// recommendationDisplayStatusFromExtra reads the display status the API serves, falling back to
// deriving one while the generated SDK still delivers these fields as extra properties.
func recommendationDisplayStatusFromExtra(raw string, extra map[string]interface{}) string {
	if served := extraString(extra, "display_status"); served != "" {
		return served
	}
	if !canDeriveDisplayStatus(raw, extra) {
		return raw
	}
	acceptance, _ := extra["saving_acceptance"].(string)
	return recommendationDisplayStatus(raw, acceptance, extraString(extra, "execution_request_status"))
}

// canDeriveDisplayStatus reports whether the qualifying fields arrived at all. Only a pending row
// needs them; deriving one without them would report an accepted saving as still open.
func canDeriveDisplayStatus(raw string, extra map[string]interface{}) bool {
	if raw != statusPending {
		return true
	}
	_, hasAcceptance := extra["saving_acceptance"]
	return hasAcceptance || extraString(extra, "execution_request_status") != ""
}

// recommendationDisplayStatus resolves the word a person reads from the stored lifecycle status.
//
// The stored status alone is ambiguous: a pending saving reads as available until somebody accepts
// it, and an execution waiting on an approver outranks that split entirely.
func recommendationDisplayStatus(raw, savingAcceptance, executionRequestStatus string) string {
	if executionRequestStatus == executionRequestPendingApproval {
		return statusAwaitingApproval
	}

	switch raw {
	case statusPending:
		if savingAcceptance == savingAcceptanceAccepted {
			return statusPending
		}
		return statusAvailable
	case statusProcessing:
		return statusPending
	case statusOptimized:
		return statusSaved
	}

	return raw
}
