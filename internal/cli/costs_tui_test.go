package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func sampleCostsModel(t *testing.T) costsModel {
	t.Helper()
	svc := "EC2"
	region := "us-east-1"
	account := "111"
	prev := 450.0
	change := 5.2

	items := []*levelfourgo.ProviderServiceBreakdownItem{
		{
			Service:          &svc,
			Region:           &region,
			AccountID:        &account,
			Cost:             500.0,
			PreviousCost:     &prev,
			ChangePercentage: &change,
			SpendingsByDate: []*levelfourgo.SpendingByDateItem{
				{Date: "2026-04-01", Value: 10.0},
				{Date: "2026-04-02", Value: 15.0},
				{Date: "2026-04-03", Value: 20.0},
				{Date: "2026-04-04", Value: 18.0},
			},
		},
		{
			Service: &svc,
			Cost:    300.0,
		},
	}

	data := &levelfourgo.ProviderServiceBreakdownData{
		ProviderID:      "aws",
		ProviderName:    "AWS",
		StartDate:       "2026-04-01",
		EndDate:         "2026-04-30",
		TotalPeriodCost: 800.0,
		Items:           items,
	}

	m := newCostsModel(nil, "aws", data, items, nil, 20, costsFilterState{})
	return m
}

func readyCostsModel(t *testing.T) costsModel {
	t.Helper()
	m := sampleCostsModel(t)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return updated.(costsModel)
}

func TestCostsModelWindowSizeBootstrap(t *testing.T) {
	m := sampleCostsModel(t)
	if m.ready {
		t.Fatal("model should not be ready before WindowSizeMsg")
	}
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := updated.(costsModel)
	if !got.ready {
		t.Error("WindowSizeMsg should set ready=true")
	}
	if got.width != 120 || got.height != 40 {
		t.Errorf("width/height not propagated: %d x %d", got.width, got.height)
	}
}

func TestCostsModelDetailToggle(t *testing.T) {
	m := readyCostsModel(t)
	if m.detailOpen {
		t.Fatal("detail should start closed")
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Error("enter should open detail")
	}
	if m.detailRow == nil {
		t.Error("detailRow should be set after open")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("esc should close detail")
	}
	if m.detailRow != nil {
		t.Error("detailRow should clear after close")
	}
}

func TestCostsModelEnterTogglesWhenOpen(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Fatal("first enter should open")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("second enter should close detail")
	}
}

func TestCostsModelSearchMode(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeSearch {
		t.Errorf("expected tuicommon.ModeSearch, got %d", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Errorf("esc should return to normal mode, got %d", m.mode)
	}
}

func TestCostsModelHelpMode(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeHelp {
		t.Errorf("expected tuicommon.ModeHelp, got %d", m.mode)
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Errorf("? should toggle help off, got %d", m.mode)
	}
}

func TestCostsModelQuit(t *testing.T) {
	m := readyCostsModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q should produce a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestCostsModelEscClosesDetailDoesNotQuit(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}

	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("esc should close detail")
	}
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.QuitMsg); ok {
				t.Error("esc should not quit when detail is open")
			}
		}
	}
}

func TestCostsModelEscQuitsWhenDetailClosed(t *testing.T) {
	m := readyCostsModel(t)
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc with closed detail should produce a quit command")
	}
	msg := cmd()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Errorf("expected QuitMsg, got %T", msg)
	}
}

func TestCostsModelSortCycle(t *testing.T) {
	m := readyCostsModel(t)
	initialIdx := m.sortIdx
	updated, _ := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	m = updated.(costsModel)
	if m.sortIdx == initialIdx {
		t.Error("s should advance sort column index")
	}
	if !strings.Contains(m.feedback, "Sort:") {
		t.Errorf("expected sort feedback, got %q", m.feedback)
	}
}

func TestCostsModelSortOrderToggle(t *testing.T) {
	m := readyCostsModel(t)
	initial := m.sortOrder
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S", Mod: tea.ModShift})
	m = updated.(costsModel)
	if m.sortOrder == initial {
		t.Errorf("S should toggle sort order from %q", initial)
	}
}

func TestCostsModelViewContainsHeader(t *testing.T) {
	m := readyCostsModel(t)
	view := m.View()
	out := view.Content
	if !strings.Contains(out, "Period Total") {
		t.Errorf("view should include Period Total KPI: %q", out)
	}
	if !strings.Contains(out, "$800.00") {
		t.Errorf("view should include total period cost: %q", out)
	}
}

func TestCostsModelViewLoadingStateBeforeReady(t *testing.T) {
	m := sampleCostsModel(t)
	view := m.View()
	out := view.Content
	if !strings.Contains(out, "Loading") {
		t.Errorf("pre-ready view should show loading indicator: %q", out)
	}
}

func TestRenderSparkline(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := renderSparkline(nil, 10)
		if got != "" {
			t.Errorf("expected empty string for no points, got %q", got)
		}
	})

	t.Run("all zero", func(t *testing.T) {
		pts := []*levelfourgo.SpendingByDateItem{
			{Value: 0}, {Value: 0}, {Value: 0},
		}
		got := renderSparkline(pts, 10)
		if len([]rune(got)) != 3 {
			t.Errorf("expected 3 runes, got %d", len([]rune(got)))
		}
	})

	t.Run("varied values", func(t *testing.T) {
		pts := []*levelfourgo.SpendingByDateItem{
			{Value: 10}, {Value: 50}, {Value: 100}, {Value: 75}, {Value: 25},
		}
		got := renderSparkline(pts, 10)
		if len([]rune(got)) != 5 {
			t.Errorf("expected 5 runes, got %d", len([]rune(got)))
		}
		found := false
		for _, r := range got {
			if r == '█' {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("max value should render as full block: %q", got)
		}
	})

	t.Run("downsamples to maxWidth", func(t *testing.T) {
		pts := make([]*levelfourgo.SpendingByDateItem, 100)
		for i := range pts {
			pts[i] = &levelfourgo.SpendingByDateItem{Value: float64(i)}
		}
		got := renderSparkline(pts, 20)
		if len([]rune(got)) != 20 {
			t.Errorf("expected 20 runes after downsampling, got %d", len([]rune(got)))
		}
	})

	t.Run("mixed positive and negative clamps negative idx to zero", func(t *testing.T) {
		pts := []*levelfourgo.SpendingByDateItem{
			{Value: 100}, {Value: -50}, {Value: 80}, {Value: 50},
		}
		got := renderSparkline(pts, 4)
		runes := []rune(got)
		if len(runes) != 4 {
			t.Fatalf("expected 4 runes, got %d", len(runes))
		}
		if runes[1] != sparkBlocks[0] {
			t.Errorf("negative value should clamp to lowest block, got %q", runes[1])
		}
	})
}

func TestBucketValues(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	t.Run("no-op when len <= maxWidth", func(t *testing.T) {
		out := bucketValues(values, 10)
		if len(out) != len(values) {
			t.Errorf("expected unchanged length, got %d", len(out))
		}
	})
	t.Run("buckets when longer than maxWidth", func(t *testing.T) {
		out := bucketValues(values, 4)
		if len(out) != 4 {
			t.Errorf("expected length 4, got %d", len(out))
		}
		if out[0] != 1.5 {
			t.Errorf("first bucket avg of [1,2] = 1.5, got %v", out[0])
		}
	})
}

func TestDetailDimensionsFiltersNil(t *testing.T) {
	svc := "EC2"
	empty := ""
	item := &levelfourgo.ProviderServiceBreakdownItem{
		Service:     &svc,
		Environment: &empty,
	}
	dims := detailDimensions(item)
	if len(dims) != 1 {
		t.Errorf("expected 1 dimension (Service only), got %d: %+v", len(dims), dims)
	}
	if dims[0].label != "Service" {
		t.Errorf("expected Service dimension, got %q", dims[0].label)
	}
}

func TestFormatDetailHeader(t *testing.T) {
	svc := "EC2"
	region := "us-east-1"
	account := "111"

	t.Run("full dims", func(t *testing.T) {
		item := &levelfourgo.ProviderServiceBreakdownItem{
			Service:   &svc,
			Region:    &region,
			AccountID: &account,
		}
		got := formatDetailHeader(item)
		for _, want := range []string{"EC2", "us-east-1", "111"} {
			if !strings.Contains(got, want) {
				t.Errorf("header missing %q: %q", want, got)
			}
		}
	})

	t.Run("empty item falls back", func(t *testing.T) {
		got := formatDetailHeader(&levelfourgo.ProviderServiceBreakdownItem{})
		if got == "" {
			t.Error("expected non-empty fallback")
		}
	})

	t.Run("tag dims formatted with equals", func(t *testing.T) {
		tk := "Env"
		tv := "prod"
		item := &levelfourgo.ProviderServiceBreakdownItem{
			TagKey:   &tk,
			TagValue: &tv,
		}
		got := formatDetailHeader(item)
		if !strings.Contains(got, "Env=prod") {
			t.Errorf("expected 'Env=prod' in header, got %q", got)
		}
	})
}

func TestCostsModelFilterClosesDetailPane(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("entering filter mode should close detail pane")
	}
	if m.mode != tuicommon.ModeFilter {
		t.Errorf("expected ModeFilter, got %d", m.mode)
	}
}

func TestCostsModelFilterModeEntry(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeFilter {
		t.Errorf("f should enter filter mode, got %d", m.mode)
	}
}

func TestCostsModelFilterModeCancel(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(costsModel)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Errorf("esc should leave filter mode, got %d", m.mode)
	}
}

func TestCostsModelFilterCycleDim(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(costsModel)
	initialDim := m.filterDim
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m = updated.(costsModel)
	if m.filterDim == initialDim {
		t.Error("tab should cycle filter dimension")
	}
	expected := (initialDim + 1) % len(costsFilterDims)
	if m.filterDim != expected {
		t.Errorf("expected filterDim=%d, got %d", expected, m.filterDim)
	}
}

func TestCostsModelFilterApply(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'f', Text: "f"})
	m = updated.(costsModel)
	m.filterInput.SetValue("EC2")

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)

	if m.mode != tuicommon.ModeNormal {
		t.Errorf("enter should return to normal mode, got %d", m.mode)
	}
	if !containsString(m.baseState.service, "EC2") {
		t.Errorf("expected service=EC2 in baseState, got %v", m.baseState.service)
	}
	if !strings.Contains(m.feedback, "service=EC2") {
		t.Errorf("expected feedback to show applied filter, got %q", m.feedback)
	}
}

func TestCostsModelFilterClearAll(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.service = []string{"EC2"}
	m.baseState.region = []string{"us-east-1"}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'F', Text: "F", Mod: tea.ModShift})
	m = updated.(costsModel)

	if len(m.baseState.service) != 0 || len(m.baseState.region) != 0 {
		t.Errorf("F should clear all filters, got service=%v region=%v",
			m.baseState.service, m.baseState.region)
	}
	if !strings.Contains(m.feedback, "cleared") {
		t.Errorf("expected cleared feedback, got %q", m.feedback)
	}
}

func TestActiveFilterSummary(t *testing.T) {
	s := costsFilterState{
		service: []string{"EC2", "RDS"},
		region:  []string{"us-east-1"},
	}
	got := activeFilterSummary(s)
	for _, want := range []string{"service=EC2,RDS", "region=us-east-1"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected %q in summary %q", want, got)
		}
	}

	empty := costsFilterState{}
	if activeFilterSummary(empty) != "" {
		t.Error("empty state should produce empty summary")
	}
}

func TestCostsModelSearchFilter(t *testing.T) {
	m := readyCostsModel(t)
	m.searchQuery = "us-east-1"
	m.applySearch()
	if len(m.items) != 1 {
		t.Errorf("expected 1 item matching 'us-east-1', got %d", len(m.items))
	}

	m.searchQuery = "does-not-exist"
	m.applySearch()
	if len(m.items) != 0 {
		t.Errorf("expected 0 items for bogus search, got %d", len(m.items))
	}

	m.searchQuery = ""
	m.applySearch()
	if len(m.items) != 2 {
		t.Errorf("empty search should restore all items, got %d", len(m.items))
	}
}
