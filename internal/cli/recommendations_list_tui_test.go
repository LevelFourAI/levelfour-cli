package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func testOverview() *levelfourgo.RecommendationsOverviewData {
	o := &levelfourgo.RecommendationsOverviewData{}
	o.SetTotalSpend(31443.84)
	o.SetAvailableSavings(13287.04)
	o.SetPendingSavings(1900.00)
	o.SetSavedItd(691.20)
	return o
}

func testItems() []*levelfourgo.ProviderBreakdownItem {
	env1 := "production"
	env2 := "staging"
	acct := "123456789012"
	tag := "Squad-Platform"
	author := "bruno@levelfour.ai"
	statusAvailable := levelfourgo.ProviderBreakdownItemStatus("available")
	statusPending := levelfourgo.ProviderBreakdownItemStatus("pending")

	item1 := &levelfourgo.ProviderBreakdownItem{}
	item1.SetRecommendationID("REC-001")
	item1.SetService("EC2")
	item1.SetEnvironment(&env1)
	item1.SetAccount(&acct)
	item1.SetTag(&tag)
	item1.SetMonthlySavings(150.00)
	item1.SetSavingsPercentage(30.0)
	item1.SetStatus(&statusAvailable)
	item1.SetSavingAcceptedBy(&author)

	item2 := &levelfourgo.ProviderBreakdownItem{}
	item2.SetRecommendationID("REC-002")
	item2.SetService("RDS")
	item2.SetEnvironment(&env2)
	item2.SetMonthlySavings(250.00)
	item2.SetSavingsPercentage(45.0)
	item2.SetStatus(&statusPending)

	return []*levelfourgo.ProviderBreakdownItem{item1, item2}
}

func testPagination() *levelfourgo.PaginationMeta {
	p := &levelfourgo.PaginationMeta{}
	p.SetCurrentPage(1)
	p.SetTotalPages(3)
	p.SetTotalItems(52)
	p.SetPageSize(20)
	p.SetHasNext(true)
	p.SetHasPrevious(false)
	return p
}

func readyListModel(t *testing.T) recommendationsListModel {
	t.Helper()
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	return updated.(recommendationsListModel)
}

func TestListModelConstructor(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)

	if m.providerID != "aws" {
		t.Errorf("providerID = %q, want aws", m.providerID)
	}
	if len(m.items) != 2 {
		t.Errorf("items count = %d, want 2", len(m.items))
	}
	if m.page != 1 {
		t.Errorf("page = %d, want 1", m.page)
	}
	if m.ready {
		t.Error("model should not be ready before WindowSizeMsg")
	}
}

func TestListModelWindowSize(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationsListModel)

	if !m2.ready {
		t.Error("model should be ready after WindowSizeMsg")
	}
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
}

func TestListModelTableNavigation(t *testing.T) {
	m := readyListModel(t)

	if m.table.Cursor() != 0 {
		t.Errorf("initial cursor = %d, want 0", m.table.Cursor())
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m2 := updated.(recommendationsListModel)
	if m2.table.Cursor() != 1 {
		t.Errorf("after down, cursor = %d, want 1", m2.table.Cursor())
	}

	updated, _ = m2.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	m3 := updated.(recommendationsListModel)
	if m3.table.Cursor() != 0 {
		t.Errorf("after up, cursor = %d, want 0", m3.table.Cursor())
	}
}

func TestListModelEnterSelectsRow(t *testing.T) {
	m := readyListModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(recommendationsListModel)

	if m2.selected != "REC-001" {
		t.Errorf("selected = %q, want REC-001", m2.selected)
	}
}

func TestListModelSearchMode(t *testing.T) {
	m := readyListModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: '/'})
	m2 := updated.(recommendationsListModel)
	if m2.mode != tuicommon.ModeSearch {
		t.Error("expected search mode after /")
	}

	updated, _ = m2.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m3 := updated.(recommendationsListModel)
	if m3.mode != tuicommon.ModeNormal {
		t.Error("expected normal mode after Esc in search")
	}
}

func TestListModelSearchFilters(t *testing.T) {
	m := readyListModel(t)

	m.searchQuery = "RDS"
	m.applySearch()

	if len(m.items) != 1 {
		t.Errorf("filtered items = %d, want 1", len(m.items))
	}
	if m.items[0].GetRecommendationID() != "REC-002" {
		t.Errorf("filtered item = %q, want REC-002", m.items[0].GetRecommendationID())
	}

	m.searchQuery = ""
	m.applySearch()
	if len(m.items) != 2 {
		t.Errorf("after clearing search, items = %d, want 2", len(m.items))
	}
}

func TestListModelSearchByHiddenField(t *testing.T) {
	m := readyListModel(t)

	m.searchQuery = "123456789012"
	m.applySearch()

	if len(m.items) != 1 {
		t.Errorf("search by account should find 1 item, got %d", len(m.items))
	}
}

func TestListModelSortCycle(t *testing.T) {
	m := readyListModel(t)

	if m.sortIdx != 0 {
		t.Errorf("initial sortIdx = %d, want 0", m.sortIdx)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 's'})
	m2 := updated.(recommendationsListModel)
	if m2.sortIdx != 1 {
		t.Errorf("after s, sortIdx = %d, want 1", m2.sortIdx)
	}
}

func TestListModelSortOrderToggle(t *testing.T) {
	m := readyListModel(t)

	if m.sortOrder != "desc" {
		t.Errorf("initial sortOrder = %q, want desc", m.sortOrder)
	}

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'S'})
	m2 := updated.(recommendationsListModel)
	if m2.sortOrder != "asc" {
		t.Errorf("after S, sortOrder = %q, want asc", m2.sortOrder)
	}
}

func TestListModelHelpMode(t *testing.T) {
	m := readyListModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: '?'})
	m2 := updated.(recommendationsListModel)
	if m2.mode != tuicommon.ModeHelp {
		t.Error("expected help mode after ?")
	}

	updated, _ = m2.Update(tea.KeyPressMsg{Code: '?'})
	m3 := updated.(recommendationsListModel)
	if m3.mode != tuicommon.ModeNormal {
		t.Error("expected normal mode after second ?")
	}
}

func TestListModelViewRendersKPI(t *testing.T) {
	m := readyListModel(t)
	view := m.View()
	content := view.Content

	if len(content) == 0 {
		t.Fatal("view should not be empty")
	}
	if !containsText(content, "Total Spend") {
		t.Error("view missing Total Spend KPI")
	}
	if !containsText(content, "Available Savings") {
		t.Error("view missing Available Savings KPI")
	}
}

func TestListModelViewRendersTable(t *testing.T) {
	m := readyListModel(t)
	view := m.View()
	content := view.Content

	if !containsText(content, "REC-001") {
		t.Error("view missing REC-001")
	}
	if !containsText(content, "EC2") {
		t.Error("view missing EC2 service")
	}
	if !containsText(content, "REC-002") {
		t.Error("view missing REC-002")
	}
}

func TestListModelViewRendersPagination(t *testing.T) {
	m := readyListModel(t)
	view := m.View()
	content := view.Content

	if !containsText(content, "Page 1/3") {
		t.Error("view missing pagination info")
	}
	if !containsText(content, "52 items") {
		t.Error("view missing total items")
	}
}

func TestListModelAdaptiveColumns(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	wide := updated.(recommendationsListModel)
	wideCols := wide.buildColumns()

	hasAuthor := false
	hasAccount := false
	for _, c := range wideCols {
		if c.Title == "Author" {
			hasAuthor = true
		}
		if c.Title == "Account" {
			hasAccount = true
		}
	}
	if !hasAuthor {
		t.Error("wide terminal should show Author column")
	}
	if !hasAccount {
		t.Error("wide terminal should show Account column")
	}

	updated, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 40})
	narrow := updated.(recommendationsListModel)
	narrowCols := narrow.buildColumns()

	for _, c := range narrowCols {
		if c.Title == "Author" || c.Title == "Account" || c.Title == "Tag" {
			t.Errorf("narrow terminal should not show %s column", c.Title)
		}
	}
}

func TestListModelInit(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return spinner tick when not ready")
	}

	m2 := readyListModel(t)
	cmd2 := m2.Init()
	if cmd2 != nil {
		t.Error("Init should return nil when ready")
	}
}

func TestListModelFullHelp(t *testing.T) {
	bindings := listKeys.FullHelp()
	if len(bindings) != 4 {
		t.Errorf("FullHelp should return 4 groups, got %d", len(bindings))
	}
}

func TestListModelSpinnerTick(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	m.loading = true
	_, cmd := m.Update(spinner.TickMsg{})
	if cmd == nil {
		t.Error("spinner tick should return a cmd when loading")
	}
}

func TestListModelFetchResult(t *testing.T) {
	m := readyListModel(t)
	m.loading = true

	newPagination := testPagination()
	newPagination.SetCurrentPage(2)
	newItems := testItems()[:1]

	updated, _ := m.Update(listFetchMsg{
		items:      newItems,
		pagination: newPagination,
	})
	m2 := updated.(recommendationsListModel)

	if m2.loading {
		t.Error("loading should be false after fetch result")
	}
	if len(m2.items) != 1 {
		t.Errorf("items = %d, want 1", len(m2.items))
	}
	if m2.page != 2 {
		t.Errorf("page = %d, want 2", m2.page)
	}
}

func TestListModelFetchError(t *testing.T) {
	m := readyListModel(t)
	m.loading = true

	updated, _ := m.Update(listFetchMsg{err: fmt.Errorf("network error")})
	m2 := updated.(recommendationsListModel)

	if m2.loading {
		t.Error("loading should be false after error")
	}
	if !strings.Contains(m2.feedback, "network error") {
		t.Errorf("feedback should contain error: %q", m2.feedback)
	}
}

func TestListModelSearchCancel(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchQuery = "RDS"
	m.applySearch()

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m2 := updated.(recommendationsListModel)

	if m2.mode != tuicommon.ModeNormal {
		t.Error("should exit search on Esc")
	}
	if m2.searchQuery != "" {
		t.Error("search query should be cleared on cancel")
	}
	if len(m2.items) != 2 {
		t.Errorf("items should be restored: got %d", len(m2.items))
	}
}

func TestListModelSearchConfirm(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.SetValue("EC2")

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m2 := updated.(recommendationsListModel)

	if m2.mode != tuicommon.ModeNormal {
		t.Error("should exit search on Enter")
	}
	if m2.searchQuery != "EC2" {
		t.Errorf("searchQuery = %q, want EC2", m2.searchQuery)
	}
}

func TestListModelHelpQuit(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeHelp

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Error("q in help mode should quit")
	}
}

func TestListModelViewLoading(t *testing.T) {
	m := readyListModel(t)
	m.loading = true
	view := m.View()
	if !strings.Contains(view.Content, "Loading") {
		t.Error("view should show loading when loading=true")
	}
}

func TestListModelViewSearchMode(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.Focus()
	view := m.View()
	if !strings.Contains(view.Content, "/") {
		t.Error("view should show search input in search mode")
	}
}

func TestListModelViewHelpMode(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeHelp
	view := m.View()
	if !containsText(view.Content, "Keyboard Shortcuts") {
		t.Error("view should show help in help mode")
	}
}

func TestListModelNilOverviewKPI(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", nil, testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 40})
	m2 := updated.(recommendationsListModel)
	view := m2.View()
	if len(view.Content) == 0 {
		t.Error("view should render even with nil overview")
	}
}

func TestListModelOpenBrowser(t *testing.T) {
	m := readyListModel(t)

	origBrowser := openBrowser
	var opened string
	openBrowser = func(url string) error { opened = url; return nil }
	defer func() { openBrowser = origBrowser }()

	m.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	if opened == "" {
		t.Error("o key should open browser")
	}
}

func TestListModelSortArrow(t *testing.T) {
	m := readyListModel(t)
	if m.sortArrow() != "↓" {
		t.Errorf("desc arrow = %q, want ↓", m.sortArrow())
	}
	m.sortOrder = "asc"
	if m.sortArrow() != "↑" {
		t.Errorf("asc arrow = %q, want ↑", m.sortArrow())
	}
}

func TestListModelHandlePageNavNext(t *testing.T) {
	m := readyListModel(t)

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m2 := updated.(recommendationsListModel)

	if !m2.loading {
		t.Error("loading should be true after next page")
	}
	if m2.page != 2 {
		t.Errorf("page = %d, want 2", m2.page)
	}
	if cmd == nil {
		t.Error("next page should return a cmd")
	}
}

func TestListModelHandlePageNavPrev(t *testing.T) {
	m := readyListModel(t)
	m.page = 2
	p := testPagination()
	p.SetCurrentPage(2)
	p.SetHasPrevious(true)
	m.pagination = p

	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m2 := updated.(recommendationsListModel)

	if !m2.loading {
		t.Error("loading should be true after prev page")
	}
	if m2.page != 1 {
		t.Errorf("page = %d, want 1", m2.page)
	}
	if cmd == nil {
		t.Error("prev page should return a cmd")
	}
}

func TestListModelHandlePageNavNoNext(t *testing.T) {
	m := readyListModel(t)
	p := testPagination()
	p.SetHasNext(false)
	m.pagination = p

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m2 := updated.(recommendationsListModel)

	if m2.loading {
		t.Error("should not load when no next page")
	}
}

func TestListModelHandlePageNavNoPrev(t *testing.T) {
	m := readyListModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	m2 := updated.(recommendationsListModel)

	if m2.loading {
		t.Error("should not load when no previous page")
	}
}

func TestListModelHandleSortAscToDesc(t *testing.T) {
	m := readyListModel(t)
	m.sortOrder = "asc"

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	m2 := updated.(recommendationsListModel)

	if m2.sortOrder != sortOrderDesc {
		t.Errorf("sortOrder = %q, want desc", m2.sortOrder)
	}
}

func TestListModelUpdateSearchTyping(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.Focus()

	updated, _ := m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m2 := updated.(recommendationsListModel)

	if m2.mode != tuicommon.ModeSearch {
		t.Error("should stay in search mode while typing")
	}
}

func TestListModelHelpEsc(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeHelp

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m2 := updated.(recommendationsListModel)
	if m2.mode != tuicommon.ModeNormal {
		t.Error("Esc should exit help mode")
	}
}

func TestListModelBuildColumnsNarrow(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 40})
	m2 := updated.(recommendationsListModel)

	cols := m2.buildColumns()
	for _, c := range cols {
		if c.Title == "Environment" || c.Title == "Savings %" {
			t.Errorf("width 90 should not show %s column", c.Title)
		}
	}
}

func TestListModelBuildTableSmallHeight(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 8})
	m2 := updated.(recommendationsListModel)

	view := m2.View()
	if len(view.Content) == 0 {
		t.Error("should render even with small height")
	}
}

func TestListModelViewNotReady(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	view := m.View()
	if !strings.Contains(view.Content, "Loading") {
		t.Error("not-ready view should show loading")
	}
}

func TestListModelRenderKPIHeaderNarrow(t *testing.T) {
	m := readyListModel(t)
	m.width = 60
	header := m.renderKPIHeader()
	if len(header) == 0 {
		t.Error("KPI header should render even on narrow terminal")
	}
}

func TestListModelQuitFromNormal(t *testing.T) {
	m := readyListModel(t)

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Error("q should return quit cmd")
	}
}

func TestListModelTableDelegation(t *testing.T) {
	m := readyListModel(t)

	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m2 := updated.(recommendationsListModel)

	updated, _ = m2.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m3 := updated.(recommendationsListModel)
	_ = m3
}

func TestListModelFetchCurrentPageReturnsCmd(t *testing.T) {
	m := readyListModel(t)
	cmd := m.fetchCurrentPage()
	if cmd == nil {
		t.Error("fetchCurrentPage should return a non-nil cmd")
	}
}

func TestListModelFetchCurrentPageExecute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/recommendations") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 100,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id": "FETCH-001", "service": "S3",
							"monthly_savings": 100, "savings_percentage": 50,
							"status": "available",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 2, "total_pages": 3, "total_items": 52,
						"page_size": 20, "has_next": true, "has_previous": true,
					},
				},
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	client, err := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "test")
	if err != nil {
		t.Fatalf("failed to create SDK client: %v", err)
	}

	m := readyListModel(t)
	m.client = client
	m.page = 2

	cmd := m.fetchCurrentPage()
	msg := cmd()
	result, ok := msg.(listFetchMsg)
	if !ok {
		t.Fatalf("expected listFetchMsg, got %T", msg)
	}
	if result.err != nil {
		t.Fatalf("fetch error: %v", result.err)
	}
	if len(result.items) != 1 {
		t.Errorf("items = %d, want 1", len(result.items))
	}
	if result.items[0].GetRecommendationID() != "FETCH-001" {
		t.Errorf("id = %q, want FETCH-001", result.items[0].GetRecommendationID())
	}
}

func TestListModelFetchCurrentPageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	client, err := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "test")
	if err != nil {
		t.Fatalf("failed to create SDK client: %v", err)
	}

	m := readyListModel(t)
	m.client = client

	cmd := m.fetchCurrentPage()
	msg := cmd()
	result, ok := msg.(listFetchMsg)
	if !ok {
		t.Fatalf("expected listFetchMsg, got %T", msg)
	}
	if result.err == nil {
		t.Error("expected error from failed API call")
	}
}

func TestListModelSpinnerNotReadyNotLoading(t *testing.T) {
	m := readyListModel(t)
	_, cmd := m.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Error("spinner tick when ready and not loading should return nil")
	}
}

func TestListModelViewPaginationWithSearch(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchQuery = "EC2"
	m.searchInput.Focus()
	m.applySearch()

	view := m.View()
	if !strings.Contains(view.Content, "matches") {
		t.Error("search mode should show match count")
	}
}

func TestListModelUpdateUnknownMsg(t *testing.T) {
	m := readyListModel(t)
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})
	m2 := updated.(recommendationsListModel)
	_ = m2
	_ = cmd
}

func TestListModelUpdateUnknownMsgNotReady(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	type customMsg struct{}
	updated, cmd := m.Update(customMsg{})
	m2 := updated.(recommendationsListModel)
	if cmd != nil {
		t.Error("unhandled msg when not ready should return nil cmd")
	}
	_ = m2
}

func TestListModelUpdateKeyInHelp(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeHelp
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m2 := updated.(recommendationsListModel)
	if m2.mode != tuicommon.ModeHelp {
		t.Error("unknown key in help should stay in help")
	}
}

func TestListModelUpdateKeyInSearch(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.Focus()
	updated, _ := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	m2 := updated.(recommendationsListModel)
	if m2.mode != tuicommon.ModeSearch {
		t.Error("typing in search should stay in search")
	}
}

func TestListModelBuildColumnsVeryNarrow(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 40})
	m2 := updated.(recommendationsListModel)
	cols := m2.buildColumns()
	if len(cols) < 1 {
		t.Error("should produce at least 1 column even on narrow terminal")
	}
	for _, c := range cols {
		if c.Width < 5 {
			t.Errorf("column %q width %d should be >= 5", c.Title, c.Width)
		}
	}
}

func TestListModelBuildColumnsWidth100(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 105, Height: 40})
	m2 := updated.(recommendationsListModel)
	cols := m2.buildColumns()
	hasEnv := false
	for _, c := range cols {
		if c.Title == "Environment" {
			hasEnv = true
		}
	}
	if !hasEnv {
		t.Error("width 105 should show Environment column")
	}
}

func TestListModelBuildColumnsWidth160(t *testing.T) {
	m := newRecommendationsListModel(nil, "aws", testOverview(), testItems(), testPagination(), 20)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 165, Height: 40})
	m2 := updated.(recommendationsListModel)
	cols := m2.buildColumns()
	hasTag := false
	hasAuthor := false
	for _, c := range cols {
		if c.Title == "Tag" {
			hasTag = true
		}
		if c.Title == "Author" {
			hasAuthor = true
		}
	}
	if !hasTag {
		t.Error("width 165 should show Tag column")
	}
	if !hasAuthor {
		t.Error("width 165 should show Author column")
	}
}

func TestListModelRenderListHelp(t *testing.T) {
	m := readyListModel(t)
	help := m.renderListHelp()
	if len(help) == 0 {
		t.Error("renderListHelp should produce output")
	}
}

func TestListModelRenderListHelpSmall(t *testing.T) {
	m := readyListModel(t)
	m.height = 7
	help := m.renderListHelp()
	if len(help) == 0 {
		t.Error("renderListHelp should work with small height")
	}
}

func TestListModelViewWithFeedback(t *testing.T) {
	m := readyListModel(t)
	m.feedback = "Sort: savings $ ↓"
	view := m.View()
	if !containsText(view.Content, "savings") {
		t.Error("view should show feedback message")
	}
}

func TestListModelViewSearchWithQuery(t *testing.T) {
	m := readyListModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchQuery = "EC2"
	m.searchInput.SetValue("EC2")
	m.searchInput.Focus()
	m.applySearch()
	view := m.View()
	if !strings.Contains(view.Content, "matches") {
		t.Error("search mode with query should show match count")
	}
}

func TestListModelViewNoPagination(t *testing.T) {
	m := readyListModel(t)
	m.pagination = nil
	view := m.View()
	if len(view.Content) == 0 {
		t.Error("view should render without pagination")
	}
}

func TestListModelFetchResultNilPagination(t *testing.T) {
	m := readyListModel(t)
	m.loading = true

	updated, _ := m.Update(listFetchMsg{
		items:      testItems(),
		pagination: nil,
	})
	m2 := updated.(recommendationsListModel)
	if m2.page != 1 {
		t.Errorf("page should stay at 1 when pagination is nil, got %d", m2.page)
	}
}

func TestListModelBuildRowsAllWidths(t *testing.T) {
	m := readyListModel(t)

	m.width = 80
	rows80 := m.buildRows()
	if len(rows80) != 2 {
		t.Errorf("80w rows = %d, want 2", len(rows80))
	}
	if len(rows80[0]) != 4 {
		t.Errorf("80w cols per row = %d, want 4", len(rows80[0]))
	}

	m.width = 110
	rows110 := m.buildRows()
	if len(rows110[0]) != 6 {
		t.Errorf("110w cols per row = %d, want 6", len(rows110[0]))
	}

	m.width = 150
	rows150 := m.buildRows()
	if len(rows150[0]) != 7 {
		t.Errorf("150w cols per row = %d, want 7", len(rows150[0]))
	}

	m.width = 200
	rows200 := m.buildRows()
	if len(rows200[0]) != 9 {
		t.Errorf("200w cols per row = %d, want 9", len(rows200[0]))
	}
}

func containsText(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && contains(s, substr)
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		si := i
		for j := 0; j < len(substr); j++ {
			for si < len(s) && (s[si] < 32 || s[si] == 27) {
				for si < len(s) && s[si] == 27 {
					si++
					for si < len(s) && (s[si] < 'A' || s[si] > 'Z') && (s[si] < 'a' || s[si] > 'z') {
						si++
					}
					if si < len(s) {
						si++
					}
				}
				if si < len(s) && s[si] < 32 && s[si] != 27 {
					si++
				}
			}
			if si >= len(s) {
				match = false
				break
			}
			if s[si] != substr[j] {
				match = false
				break
			}
			si++
		}
		if match {
			return true
		}
	}
	return false
}
