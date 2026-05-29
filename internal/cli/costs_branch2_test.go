package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestUpdateCostsSearchConfirmWithValue(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.SetValue("EC2")
	m.searchQuery = "EC2"
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Errorf("enter should leave search mode, got %d", m.mode)
	}
	if m.searchQuery != "EC2" {
		t.Errorf("query should persist, got %q", m.searchQuery)
	}
}

func TestBuildTableColumnsVeryNarrow(t *testing.T) {
	m := readyCostsModel(t)
	m.width = 1
	cols := m.buildTableColumns()
	for _, c := range cols {
		if c.Width < 5 {
			t.Errorf("column width %d below minimum 5", c.Width)
		}
	}
}

func TestViewWithPagination(t *testing.T) {
	m := readyCostsModel(t)
	m.pagination = &levelfourgo.PaginationMeta{
		CurrentPage: 1,
		TotalPages:  3,
		TotalItems:  50,
	}
	view := m.View().Content
	if !strings.Contains(view, "Page 1/3") {
		t.Errorf("footer should include pagination: %q", view)
	}
}

func TestViewFeedbackBranch(t *testing.T) {
	m := readyCostsModel(t)
	m.feedback = "Sort: cost ↓"
	view := m.View().Content
	if !strings.Contains(view, "Sort: cost") {
		t.Errorf("footer should show feedback: %q", view)
	}
}

func TestRenderCostsKPIHeaderTinyTerminal(t *testing.T) {
	m := readyCostsModel(t)
	m.width = 10
	got := m.renderCostsKPIHeader()
	if got == "" {
		t.Error("narrow header should still render")
	}
}

func TestViewRowsFromItemsFallback(t *testing.T) {
	m := readyCostsModel(t)
	m.pagination = nil
	view := m.View().Content
	_ = view
}

func TestRenderFilterDrawerActiveFilters(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.service = []string{"EC2"}
	got := m.renderFilterDrawer()
	if !strings.Contains(got, "Active:") {
		t.Errorf("drawer should show active filters when present: %q", got)
	}
}

func TestRenderSparklineHighPrecision(t *testing.T) {
	pts := make([]*levelfourgo.SpendingByDateItem, 8)
	for i := range pts {
		pts[i] = &levelfourgo.SpendingByDateItem{Value: float64(i)}
	}
	got := renderSparkline(pts, 8)
	if len([]rune(got)) != 8 {
		t.Errorf("expected 8 runes, got %d", len([]rune(got)))
	}
}

func TestRenderSparklineNegativeValues(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{
		{Value: -1}, {Value: 5}, {Value: -3},
	}
	got := renderSparkline(pts, 3)
	if len([]rune(got)) == 0 {
		t.Error("should render something for mixed values")
	}
}

func TestBucketValuesOddRatio(t *testing.T) {
	out := bucketValues([]float64{1, 2, 3, 4, 5, 6, 7}, 3)
	if len(out) != 3 {
		t.Errorf("expected 3 buckets, got %d", len(out))
	}
}

func TestBucketValuesTightRatio(t *testing.T) {
	out := bucketValues([]float64{1, 2, 3, 4, 5}, 5)
	if len(out) != 5 {
		t.Errorf("same-size should be no-op, got %d", len(out))
	}
}

func TestFormatSparklineRangeDescending(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{
		{Date: "d1", Value: 100},
		{Date: "d2", Value: 50},
		{Date: "d3", Value: 10},
	}
	got := formatSparklineRange(pts)
	if !strings.Contains(got, "min $10") {
		t.Errorf("should find min in descending sequence: %q", got)
	}
}

func TestDetailAreaHeightMinClamp(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 25
	h := m.detailAreaHeight()
	if h < 6 {
		t.Errorf("detail height should clamp at 6 minimum, got %d", h)
	}
}

func TestViewDetailCloseOnEscWhileOpen(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("esc should close detail")
	}
	view := m.View().Content
	if strings.Contains(view, "Timeline") {
		t.Error("detail content should not render after close")
	}
}

func TestClearAllFiltersWithExistingFilters(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.service = []string{"EC2"}
	m.baseState.region = []string{"us-east-1"}
	m.baseState.tagKey = []string{"Env"}

	updated, cmd := m.clearAllFilters()
	m = updated.(costsModel)
	if cmd == nil {
		t.Fatal("clear with active filters should produce refetch cmd")
	}
	for _, dim := range costsFilterDims {
		if vals := dim.get(&m.baseState); len(*vals) != 0 {
			t.Errorf("dim %s should be cleared: %v", dim.label, *vals)
		}
	}
}
