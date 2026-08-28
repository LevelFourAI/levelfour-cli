package mcp

import (
	"strings"
	"testing"
)

func TestClampPageSize(t *testing.T) {
	cases := map[int]int{0: 1, -5: 1, 10: 10, 50: 50, 999: maxPageSize}
	for in, want := range cases {
		if got := clampPageSize(in); got != want {
			t.Errorf("clampPageSize(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestPageEnvelopeWithTotal(t *testing.T) {
	total := 25
	got := pageEnvelope(pageSpec{page: 1, size: 10}, &total, 10)
	if got["has_next_page"] != true || got["next_page"] != 2 || got["total"] != 25 {
		t.Errorf("envelope = %v", got)
	}

	got = pageEnvelope(pageSpec{page: 3, size: 10}, &total, 5)
	if got["has_next_page"] != false || got["next_page"] != nil {
		t.Errorf("last page envelope = %v", got)
	}
}

func TestPageEnvelopeWithoutTotal(t *testing.T) {
	// With no count from the service, a full page is the only signal that
	// another page might exist.
	got := pageEnvelope(pageSpec{page: 2, size: 10}, nil, 10)
	if got["has_next_page"] != true || got["next_page"] != 3 {
		t.Errorf("full page = %v", got)
	}
	if _, present := got["total"]; present {
		t.Error("envelope invented a total")
	}

	got = pageEnvelope(pageSpec{page: 2, size: 10}, nil, 4)
	if got["has_next_page"] != false {
		t.Errorf("short page = %v", got)
	}
}

func TestPaginateStripsCompetingPaginationFields(t *testing.T) {
	listing := map[string]any{
		"items":      []any{"a", "b"},
		"total":      float64(2),
		"pages":      float64(1),
		"has_more":   false,
		"other_key":  "kept",
		"pagination": map[string]any{"total": float64(2)},
	}
	got := paginate(listing, pageSpec{page: 1, size: 10, itemsKey: "items"})
	for _, dropped := range []string{"pages", "has_more", "pagination"} {
		if _, present := got[dropped]; present {
			t.Errorf("paginate kept %q", dropped)
		}
	}
	if got["other_key"] != "kept" {
		t.Error("paginate dropped a non-pagination field")
	}
	if got["total"] != 2 || got["returned"] != 2 {
		t.Errorf("paginate = %v", got)
	}
}

func TestPaginateWithMissingItems(t *testing.T) {
	got := paginate(map[string]any{"items": "not a list"}, pageSpec{page: 1, size: 10, itemsKey: "items"})
	rows, ok := got["items"].([]any)
	if !ok || len(rows) != 0 {
		t.Errorf("items = %v, want an empty list", got["items"])
	}
}

func TestTotalFrom(t *testing.T) {
	cases := []struct {
		name    string
		listing map[string]any
		want    *int
	}{
		{"total", map[string]any{"total": float64(7)}, intPtr(7)},
		{"total_count", map[string]any{"total_count": float64(3)}, intPtr(3)},
		{"nested", map[string]any{"pagination": map[string]any{"total_count": float64(9)}}, intPtr(9)},
		{"nested not an object", map[string]any{"pagination": "no"}, nil},
		{"fractional is not a count", map[string]any{"total": 1.5}, nil},
		{"absent", map[string]any{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := totalFrom(tc.listing)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got %d, want nil", *got)
			case tc.want != nil && got == nil:
				t.Errorf("got nil, want %d", *tc.want)
			case tc.want != nil && *got != *tc.want:
				t.Errorf("got %d, want %d", *got, *tc.want)
			}
		})
	}
}

func intPtr(n int) *int { return &n }

func TestBounded(t *testing.T) {
	rows := []any{1, 2, 3, 4, 5}

	full := bounded(rows, 10, "items")
	if full["returned"] != 5 || full["total"] != 5 {
		t.Errorf("bounded under limit = %v", full)
	}
	if _, present := full["truncated"]; present {
		t.Error("bounded claimed truncation when nothing was dropped")
	}

	capped := bounded(rows, 2, "items")
	if capped["returned"] != 2 || capped["total"] != 5 {
		t.Errorf("bounded over limit = %v", capped)
	}
	if !strings.Contains(capped["truncated"].(string), "2 largest of 5") {
		t.Errorf("truncation note = %v", capped["truncated"])
	}

	if got := bounded(rows, 0, "items"); got["returned"] != 1 {
		t.Errorf("bounded with a zero limit = %v", got)
	}
	if got := bounded(rows, maxRows+100, "items"); got["returned"] != 5 {
		t.Errorf("bounded above the hard cap = %v", got)
	}
}

func TestHintIfEmpty(t *testing.T) {
	got := hintIfEmpty(map[string]any{"items": []any{}}, "items", "try this")
	if got["hint"] != "try this" {
		t.Error("empty result carried no hint")
	}

	got = hintIfEmpty(map[string]any{"items": []any{1}}, "items", "try this")
	if _, present := got["hint"]; present {
		t.Error("non-empty result carried a hint")
	}

	got = hintIfEmpty(map[string]any{"items": "not a list"}, "items", "try this")
	if _, present := got["hint"]; present {
		t.Error("non-list rows carried a hint")
	}
}

func TestFitBudgetLeavesSmallResultsAlone(t *testing.T) {
	result := map[string]any{"items": []any{"a"}}
	if got := fitBudget(result, "items"); len(got["items"].([]any)) != 1 {
		t.Errorf("fitBudget trimmed a small result: %v", got)
	}
}

func TestFitBudgetTrimsRows(t *testing.T) {
	row := strings.Repeat("x", 1000)
	rows := make([]any, 200)
	for i := range rows {
		rows[i] = row
	}
	got := fitBudget(map[string]any{"items": rows}, "items")
	kept := got["items"].([]any)
	if len(kept) >= len(rows) {
		t.Fatalf("fitBudget kept %d of %d rows", len(kept), len(rows))
	}
	if sizeOf(got) > maxResultChars {
		t.Errorf("trimmed result is still %d chars", sizeOf(got))
	}
	if !strings.Contains(got["truncated"].(string), "of 200 rows") {
		t.Errorf("truncation note = %v", got["truncated"])
	}
}

func TestFitBudgetWithNothingToTrim(t *testing.T) {
	// One field too large to trim: an oversized payload with no row list stays
	// as it is rather than being silently emptied.
	big := map[string]any{"blob": strings.Repeat("y", maxResultChars+10)}
	got := fitBudget(big, "items")
	if got["blob"] == nil {
		t.Error("fitBudget dropped a payload it could not trim")
	}

	got = fitBudget(map[string]any{"blob": strings.Repeat("y", maxResultChars+10), "items": []any{}}, "items")
	if _, present := got["truncated"]; present {
		t.Error("fitBudget claimed truncation with no rows to drop")
	}
}

func TestSizeOfUnmarshalableIsOverBudget(t *testing.T) {
	if got := sizeOf(map[string]any{"ch": make(chan int)}); got <= maxResultChars {
		t.Errorf("sizeOf of unmarshalable content = %d, want over budget", got)
	}
}
