package cli

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestCostsKeyMapFullHelp(t *testing.T) {
	groups := costsKeys.FullHelp()
	if len(groups) < 3 {
		t.Errorf("expected at least 3 help groups, got %d", len(groups))
	}
	for i, g := range groups {
		if len(g) == 0 {
			t.Errorf("help group %d is empty", i)
		}
	}
}

func TestCostsKeyMapShortHelp(t *testing.T) {
	short := costsKeys.ShortHelp()
	if len(short) == 0 {
		t.Error("short help should not be empty")
	}
}

func TestCostsModelInit(t *testing.T) {
	m := sampleCostsModel(t)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return spinner tick cmd when not ready")
	}

	ready := readyCostsModel(t)
	if ready.Init() != nil {
		t.Error("Init should return nil when already ready")
	}
}

func TestCostsModelUpdateTickMsg(t *testing.T) {
	m := readyCostsModel(t)
	m.loading = true
	updated, cmd := m.Update(sampleSpinnerTick())
	if _, ok := updated.(costsModel); !ok {
		t.Error("expected costsModel back")
	}
	_ = cmd

	ready := readyCostsModel(t)
	ready.loading = false
	_, cmd2 := ready.Update(sampleSpinnerTick())
	if cmd2 != nil {
		t.Error("tick when not loading should return nil cmd")
	}
}

func sampleSpinnerTick() tea.Msg {
	return testSpinnerTickMsg{}
}

type testSpinnerTickMsg struct{}

func TestCostsModelUpdateFetchError(t *testing.T) {
	m := readyCostsModel(t)
	msg := costsListFetchMsg{err: errSample("boom")}
	updated, _ := m.Update(msg)
	m = updated.(costsModel)
	if m.loading {
		t.Error("loading should be cleared on error")
	}
	if !strings.Contains(m.feedback, "boom") {
		t.Errorf("expected error feedback, got %q", m.feedback)
	}
}

type errSample string

func (e errSample) Error() string { return string(e) }

func TestCostsModelUpdateFetchSuccess(t *testing.T) {
	m := readyCostsModel(t)
	m.detailOpen = true
	m.loading = true
	svc := "EC2"
	newItem := &levelfourgo.ProviderServiceBreakdownItem{Service: &svc, Cost: 1.0}
	page := &levelfourgo.PaginationMeta{CurrentPage: 2}
	data := &levelfourgo.ProviderServiceBreakdownData{
		ProviderID: "aws",
		Items:      []*levelfourgo.ProviderServiceBreakdownItem{newItem},
		Pagination: page,
	}
	updated, _ := m.Update(costsListFetchMsg{
		data:       data,
		items:      data.Items,
		pagination: page,
	})
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("successful refetch should close detail pane")
	}
	if m.page != 2 {
		t.Errorf("expected page=2, got %d", m.page)
	}
}

func TestCostsModelPaginationNext(t *testing.T) {
	m := readyCostsModel(t)
	m.pagination = &levelfourgo.PaginationMeta{
		CurrentPage: 1, TotalPages: 3, HasNext: true,
	}
	m.page = 1
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = updated.(costsModel)
	if m.page != 2 {
		t.Errorf("n should advance page, got %d", m.page)
	}
	if cmd == nil {
		t.Error("n should produce a fetch command")
	}
}

func TestCostsModelPaginationPrev(t *testing.T) {
	m := readyCostsModel(t)
	m.pagination = &levelfourgo.PaginationMeta{
		CurrentPage: 2, TotalPages: 3, HasPrevious: true,
	}
	m.page = 2
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m = updated.(costsModel)
	if m.page != 1 {
		t.Errorf("p should go back, got %d", m.page)
	}
	if cmd == nil {
		t.Error("p should produce a fetch command")
	}
}

func TestCostsModelPaginationNoOp(t *testing.T) {
	m := readyCostsModel(t)
	m.pagination = &levelfourgo.PaginationMeta{
		CurrentPage: 1, TotalPages: 1, HasNext: false, HasPrevious: false,
	}
	m.page = 1
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m = updated.(costsModel)
	if m.page != 1 {
		t.Error("n with no next should not change page")
	}
	if cmd != nil {
		t.Error("no-op pagination should not produce command")
	}
}

func TestCostsModelOpenInBrowser(t *testing.T) {
	origBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origBrowser }()

	m := readyCostsModel(t)
	m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if !strings.Contains(openedURL, "spending") {
		t.Errorf("o should open /spending, got %q", openedURL)
	}
}

func TestCostsModelUpdateSearchTyping(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.Focus()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'E', Text: "E"})
	m = updated.(costsModel)
	if m.searchQuery == "" {
		t.Error("typing in search mode should update query")
	}
}

func TestCostsModelUpdateHelpQuit(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeHelp
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q from help mode should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("expected QuitMsg")
	}
}

func TestCostsModelUpdateHelpIgnoresOther(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeHelp
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeHelp {
		t.Error("unknown key in help mode should keep help mode")
	}
	if cmd != nil {
		t.Error("unknown key in help should produce nil cmd")
	}
}

func TestCostsModelUpdateFilterTyping(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeFilter
	m.filterInput.Focus()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	m = updated.(costsModel)
	if m.filterInput.Value() == "" {
		t.Error("typing in filter mode should accumulate in filterInput")
	}
}

func TestCostsModelUpdateFilterEmptyCommit(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeFilter
	m.filterInput.SetValue("")
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Error("empty enter should leave filter mode")
	}
	if cmd != nil {
		t.Error("empty commit should not refetch")
	}
}

func TestCostsModelUpdateFilterDuplicateValue(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.service = []string{"EC2"}
	m.mode = tuicommon.ModeFilter
	m.filterDim = 0
	m.filterInput.SetValue("EC2")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if len(m.baseState.service) != 1 {
		t.Errorf("duplicate should not be appended, got %v", m.baseState.service)
	}
}

func TestCostsModelClearAllFiltersNoop(t *testing.T) {
	m := readyCostsModel(t)
	_, cmd := m.clearAllFilters()
	if cmd != nil {
		t.Error("clear with no filters should be noop")
	}
}

func TestCostsModelToggleDetailNoSelection(t *testing.T) {
	m := readyCostsModel(t)
	m.items = nil
	updated, _ := m.toggleDetailForSelected()
	m = updated.(costsModel)
	if m.detailOpen {
		t.Error("should not open detail with no items")
	}
}

func TestCostsModelRefreshDetailFromSelected(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if !m.detailOpen {
		t.Fatal("detail should be open")
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	_ = updated
}

func TestCostsModelBuildDetailSmallHeight(t *testing.T) {
	m := sampleCostsModel(t)
	m.width = 80
	m.height = 5
	vp := m.buildDetail()
	_ = vp
}

func TestCostsModelListAreaHeightSmall(t *testing.T) {
	m := sampleCostsModel(t)
	m.width = 40
	m.height = 10
	if got := m.listAreaHeight(); got < 3 {
		t.Errorf("listAreaHeight should clamp to >= 3, got %d", got)
	}
}

func TestCostsModelDetailAreaHeightTinyScreen(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 8
	if m.detailAreaHeight() != 0 {
		t.Error("tiny screen should return 0 detail height")
	}

	m.height = 50
	if h := m.detailAreaHeight(); h == 0 {
		t.Error("large screen should have detail height")
	}
}

func TestCostsModelViewFilterMode(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeFilter
	view := m.View().Content
	if !strings.Contains(view, "Filter by") {
		t.Errorf("filter mode view should show drawer: %q", view)
	}
	if !strings.Contains(view, "tab=cycle") {
		t.Errorf("filter mode footer should show hint: %q", view)
	}
}

func TestCostsModelViewHelpMode(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeHelp
	view := m.View().Content
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Errorf("help mode should render help overlay: %q", view)
	}
}

func TestCostsModelViewSearchMode(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchQuery = "EC2"
	view := m.View().Content
	if !strings.Contains(view, "matches") {
		t.Errorf("search footer should show match count: %q", view)
	}
}

func TestCostsModelViewLoading(t *testing.T) {
	m := readyCostsModel(t)
	m.loading = true
	view := m.View().Content
	if !strings.Contains(view, "Loading") {
		t.Errorf("loading view should show indicator: %q", view)
	}
}

func TestCostsModelViewWithActiveFilters(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.service = []string{"EC2"}
	view := m.View().Content
	if !strings.Contains(view, "Filters:") {
		t.Errorf("footer should show active filters: %q", view)
	}
}

func TestCostsModelViewWithDetail(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	view := m.View().Content
	if !strings.Contains(view, "Cost:") {
		t.Errorf("detail pane should render: %q", view)
	}
}

func TestCostsModelKPIHeaderEmpty(t *testing.T) {
	m := readyCostsModel(t)
	m.breakdown = nil
	if got := m.renderCostsKPIHeader(); got != "" {
		t.Error("nil breakdown should render empty header")
	}
}

func TestCostsModelKPIHeaderNilProvider(t *testing.T) {
	m := readyCostsModel(t)
	m.breakdown = &levelfourgo.ProviderServiceBreakdownData{
		TotalPeriodCost: 0,
	}
	got := m.renderCostsKPIHeader()
	if got == "" {
		t.Error("should render header with partial data")
	}
}

func TestCostsModelDetailPaneNil(t *testing.T) {
	m := readyCostsModel(t)
	m.detailRow = nil
	if got := m.renderDetailPane(); got != "" {
		t.Error("nil detail row should render empty pane")
	}
}

func TestRenderDetailContentMinimal(t *testing.T) {
	m := readyCostsModel(t)
	item := &levelfourgo.ProviderServiceBreakdownItem{Cost: 100.0}
	content := m.renderDetailContent(item)
	if !strings.Contains(content, "Cost:") {
		t.Error("should render cost line")
	}
}

func TestRenderDetailContentNil(t *testing.T) {
	m := readyCostsModel(t)
	if m.renderDetailContent(nil) != "" {
		t.Error("nil item should produce empty content")
	}
}

func TestFormatDetailHeaderEmpty(t *testing.T) {
	got := formatDetailHeader(&levelfourgo.ProviderServiceBreakdownItem{})
	if got == "" {
		t.Error("empty item should still produce a header label")
	}
}

func TestDetailDimensionsAll(t *testing.T) {
	s, r, a, e, tk, tv := "S", "R", "A", "E", "K", "V"
	item := &levelfourgo.ProviderServiceBreakdownItem{
		Service:     &s,
		Region:      &r,
		AccountID:   &a,
		Environment: &e,
		TagKey:      &tk,
		TagValue:    &tv,
	}
	dims := detailDimensions(item)
	if len(dims) != 6 {
		t.Errorf("expected 6 dims, got %d", len(dims))
	}
}

func TestBucketValuesEdgeCases(t *testing.T) {
	out := bucketValues([]float64{1.0}, 5)
	if len(out) != 1 {
		t.Errorf("single-value input should return same slice, got %d", len(out))
	}
}

func TestFormatSparklineRangeEmpty(t *testing.T) {
	if formatSparklineRange(nil) != "" {
		t.Error("empty points should return empty string")
	}
}

func TestChangeColorAllRanges(t *testing.T) {
	if changeColor(10) == changeColor(0) {
		t.Error("positive vs neutral should differ")
	}
	if changeColor(-10) == changeColor(0) {
		t.Error("negative vs neutral should differ")
	}
	if changeColor(2) != changeColor(0) {
		t.Error("small positive should be neutral")
	}
}

func TestCostItemMatchesSearchFields(t *testing.T) {
	s, r, a, e, tk, tv := "Srv", "Reg", "Acc", "Env", "Key", "Val"
	item := &levelfourgo.ProviderServiceBreakdownItem{
		Service:     &s,
		Region:      &r,
		AccountID:   &a,
		Environment: &e,
		TagKey:      &tk,
		TagValue:    &tv,
		Cost:        123.45,
	}
	cases := []string{"srv", "reg", "acc", "env", "key", "val", "123.45"}
	for _, q := range cases {
		if !costItemMatchesSearch(item, q) {
			t.Errorf("should match query %q", q)
		}
	}
	if costItemMatchesSearch(item, "does-not-exist") {
		t.Error("should not match nonsense")
	}
}

func TestCostsSortArrowAsc(t *testing.T) {
	m := readyCostsModel(t)
	m.sortOrder = "asc"
	if m.costsSortArrow() != "↑" {
		t.Error("asc should render ↑")
	}
	m.sortOrder = sortOrderDesc
	if m.costsSortArrow() != "↓" {
		t.Error("desc should render ↓")
	}
}

func TestBuildTableColumnsPromotesGroupBy(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState.groupBy = []string{"tag"}
	m.width = 60
	cols := m.buildTableColumns()
	labels := make([]string, len(cols))
	for i, c := range cols {
		labels[i] = c.Title
	}
	hasTag := false
	for _, l := range labels {
		if strings.Contains(l, "Tag") {
			hasTag = true
		}
	}
	if !hasTag {
		t.Errorf("group-by tag should promote tag columns: %v", labels)
	}
}
