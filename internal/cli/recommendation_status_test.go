package cli

import (
	"encoding/json"
	"testing"

	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestRecommendationDisplayStatus(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		savingAcceptance string
		executionRequest string
		want             string
	}{
		{
			name: "unaccepted pending reads as available",
			raw:  statusPending,
			want: statusAvailable,
		},
		{
			name:             "accepted pending stays pending",
			raw:              statusPending,
			savingAcceptance: savingAcceptanceAccepted,
			want:             statusPending,
		},
		{
			name:             "rejected acceptance on a pending row is still available",
			raw:              statusPending,
			savingAcceptance: statusRejected,
			want:             statusAvailable,
		},
		{
			name: "processing collapses into pending",
			raw:  statusProcessing,
			want: statusPending,
		},
		{
			name: "optimized reads as saved",
			raw:  statusOptimized,
			want: statusSaved,
		},
		{
			name:             "a request awaiting an admin outranks the raw split",
			raw:              statusPending,
			savingAcceptance: savingAcceptanceAccepted,
			executionRequest: executionRequestPendingApproval,
			want:             statusAwaitingApproval,
		},
		{
			name:             "an approved request does not override the raw status",
			raw:              statusOptimized,
			executionRequest: "approved",
			want:             statusSaved,
		},
		{
			name: "rejected passes through",
			raw:  statusRejected,
			want: statusRejected,
		},
		{
			name: "unavailable passes through",
			raw:  statusUnavailable,
			want: statusUnavailable,
		},
		{
			name: "an unknown server status degrades to its raw form",
			raw:  "some_new_status",
			want: "some_new_status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := recommendationDisplayStatus(tt.raw, tt.savingAcceptance, tt.executionRequest)
			if got != tt.want {
				t.Errorf("recommendationDisplayStatus(%q, %q, %q) = %q, want %q",
					tt.raw, tt.savingAcceptance, tt.executionRequest, got, tt.want)
			}
		})
	}
}

func TestRecommendationStatusLabel(t *testing.T) {
	tests := map[string]string{
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
		"AVAILABLE":            labelAvailable,
		"unknown_status":       "unknown_status",
		"":                     "",
	}

	for status, want := range tests {
		if got := recommendationStatusLabel(status); got != want {
			t.Errorf("recommendationStatusLabel(%q) = %q, want %q", status, got, want)
		}
	}
}

func TestRecommendationStatusFromItem(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		extra map[string]interface{}
		want  string
	}{
		{
			name: "a bare pending with no acceptance field renders the raw status",
			raw:  statusPending,
			want: labelPending,
		},
		{
			name:  "nil map is safe",
			raw:   statusOptimized,
			extra: nil,
			want:  labelSaved,
		},
		{
			name:  "acceptance drives the pending split",
			raw:   statusPending,
			extra: map[string]interface{}{"saving_acceptance": savingAcceptanceAccepted},
			want:  labelPending,
		},
		{
			name:  "a pending approval wins",
			raw:   statusProcessing,
			extra: map[string]interface{}{"execution_request_status": executionRequestPendingApproval},
			want:  labelNeedsApproval,
		},
		{
			name:  "non-string extras are ignored rather than panicking",
			raw:   statusPending,
			extra: map[string]interface{}{"saving_acceptance": 42},
			want:  labelAvailable,
		},
		{
			name:  "an untouched list row is available",
			raw:   statusPending,
			extra: map[string]interface{}{"saving_acceptance": nil},
			want:  labelAvailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recommendationStatusFromItem(tt.raw, tt.extra); got != tt.want {
				t.Errorf("recommendationStatusFromItem(%q, %v) = %q, want %q", tt.raw, tt.extra, got, tt.want)
			}
		})
	}
}

func TestTheServedDisplayStatusWinsOverTheLocalDerivation(t *testing.T) {
	// The API owns this rule now. Deriving it a second time here is how the CLI and the
	// dashboard drifted apart, so what the server sent is what the table prints.
	extra := map[string]interface{}{
		"display_status":           "awaiting_approval",
		"saving_acceptance":        "accepted",
		"execution_request_status": nil,
	}

	if got := recommendationDisplayStatusFromExtra(statusPending, extra); got != statusAwaitingApproval {
		t.Errorf("display status = %q, want %q", got, statusAwaitingApproval)
	}
	if got := recommendationStatusFromItem(statusPending, extra); got != labelNeedsApproval {
		t.Errorf("label = %q, want %q", got, labelNeedsApproval)
	}
}

func TestAnEmptyServedDisplayStatusFallsBackToTheDerivation(t *testing.T) {
	// A server that has not shipped the field yet, and any non-string value in its place.
	for name, extra := range map[string]map[string]interface{}{
		"absent": {"saving_acceptance": "accepted"},
		"empty":  {"display_status": "", "saving_acceptance": "accepted"},
		"typed wrong": {
			"display_status":    7,
			"saving_acceptance": "accepted",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := recommendationDisplayStatusFromExtra(statusPending, extra); got != statusPending {
				t.Errorf("display status = %q, want %q", got, statusPending)
			}
		})
	}
}

// Every status the colour map covers must also have a label, and vice versa: a
// status with a colour but no label renders a raw enum in a coloured badge.
func TestStatusColourAndLabelMapsAgree(t *testing.T) {
	for status := range tuiStatusColors {
		if _, ok := recommendationStatusLabels[status]; !ok {
			t.Errorf("tuiStatusColors has %q with no matching label", status)
		}
	}
	for status := range recommendationStatusLabels {
		if _, ok := tuiStatusColors[status]; !ok {
			t.Errorf("recommendationStatusLabels has %q with no matching colour", status)
		}
	}
}

// The list endpoint sends saving_acceptance (null included); the detail endpoint
// omits it. Presence of the key is what separates "the server says nobody has
// accepted this" from "the server did not tell us".
func TestRecommendationDisplayStatusFromExtraRefusesToGuess(t *testing.T) {
	tests := []struct {
		name  string
		raw   string
		extra map[string]interface{}
		want  string
	}{
		{
			name:  "key present and null means untouched, so available",
			raw:   statusPending,
			extra: map[string]interface{}{"saving_acceptance": nil},
			want:  statusAvailable,
		},
		{
			name:  "key present and accepted means pending",
			raw:   statusPending,
			extra: map[string]interface{}{"saving_acceptance": savingAcceptanceAccepted},
			want:  statusPending,
		},
		{
			name:  "key absent entirely falls back to the raw status",
			raw:   statusPending,
			extra: map[string]interface{}{},
			want:  statusPending,
		},
		{
			name:  "an execution_request_status alone is enough to derive",
			raw:   statusPending,
			extra: map[string]interface{}{"execution_request_status": executionRequestPendingApproval},
			want:  statusAwaitingApproval,
		},
		{
			name:  "optimized still maps to saved with no inputs, since it needs none",
			raw:   statusOptimized,
			extra: map[string]interface{}{},
			want:  statusSaved,
		},
		{
			name:  "processing still maps to pending with no inputs",
			raw:   statusProcessing,
			extra: map[string]interface{}{},
			want:  statusPending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := recommendationDisplayStatusFromExtra(tt.raw, tt.extra); got != tt.want {
				t.Errorf("recommendationDisplayStatusFromExtra(%q, %v) = %q, want %q", tt.raw, tt.extra, got, tt.want)
			}
		})
	}
}

func TestToMapKeepsFieldsTheSdkDoesNotModelYet(t *testing.T) {
	// Every Fern model's MarshalJSON re-encodes through an embedded copy of the struct and
	// never re-emits extraProperties, so display_status vanished on the way to populateData
	// and the detail view could only ever print the raw enum.
	detail := &levelfourgo.RecommendationDetail{RecommendationID: "L4-002"}
	if err := json.Unmarshal(
		[]byte(`{"recommendation_id":"L4-002","display_status":"awaiting_approval"}`),
		detail,
	); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := toMap(detail)

	if got["display_status"] != "awaiting_approval" {
		t.Errorf("display_status = %v, want awaiting_approval", got["display_status"])
	}
	if got["recommendation_id"] != "L4-002" {
		t.Errorf("recommendation_id = %v, want L4-002", got["recommendation_id"])
	}
}

func TestToMapOnSomethingThatCarriesNoExtras(t *testing.T) {
	got := toMap(map[string]string{"a": "b"})

	if got["a"] != "b" {
		t.Errorf("toMap dropped a plain value: %v", got)
	}
}

func TestToMapOnAValueThatIsNotAnObject(t *testing.T) {
	if got := toMap("not an object"); len(got) != 0 {
		t.Errorf("toMap(%q) = %v, want an empty map", "not an object", got)
	}
}
