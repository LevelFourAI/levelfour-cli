package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
)

func fullRecommendationData() map[string]interface{} {
	return map[string]interface{}{
		"recommendation_id":  "L4-001",
		"service":            "Kubernetes",
		"environment":        "Homolog",
		"status":             "active",
		"analysis_period":    "2025-10-24",
		"created_at":         "2025-10-24T10:00:00Z",
		"current_spending":   500.00,
		"monthly_savings":    150.00,
		"annual_savings":     1800.00,
		"savings_percentage": 30.0,
		"actions": map[string]interface{}{
			"description":            "Rightsize i-12345",
			"key_takeaway":           "**Risk Level:** Low",
			"implementation_process": "## Steps\n\nModify instance type.",
			"execution_method":       "### Terraform\n\n```hcl\nresource {}\n```",
		},
	}
}

func readyModel(t *testing.T) recommendationViewModel {
	t.Helper()
	m := newRecommendationViewModel(fullRecommendationData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return updated.(recommendationViewModel)
}

func TestRecommendationViewModelConstructor(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())

	if m.recID != "L4-001" {
		t.Errorf("recID = %q, want L4-001", m.recID)
	}
	if m.service != "Kubernetes" {
		t.Errorf("service = %q, want Kubernetes", m.service)
	}
	if m.ready {
		t.Error("model should not be ready before WindowSizeMsg")
	}
	if len(m.tabs) != 0 {
		t.Errorf("tabs should be empty before WindowSizeMsg, got %d", len(m.tabs))
	}

	m2 := readyModel(t)
	if len(m2.tabs) != 3 {
		t.Fatalf("expected 3 tabs after WindowSizeMsg, got %d", len(m2.tabs))
	}
	expected := []string{"Summary", "Analysis", "Implementation"}
	for i, want := range expected {
		if m2.tabs[i].title != want {
			t.Errorf("tab[%d].title = %q, want %q", i, m2.tabs[i].title, want)
		}
	}
	if m2.tabs[1].raw != "**Risk Level:** Low" {
		t.Errorf("tab[1].raw = %q, want markdown source", m2.tabs[1].raw)
	}
}

func TestRecommendationViewModelWindowSize(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	if !m2.ready {
		t.Error("model should be ready after WindowSizeMsg")
	}
	if m2.width != 120 {
		t.Errorf("width = %d, want 120", m2.width)
	}
	if m2.height != 40 {
		t.Errorf("height = %d, want 40", m2.height)
	}
}

func TestRecommendationViewModelTabNavigation(t *testing.T) {
	m2 := readyModel(t)

	t.Run("right wraps around", func(t *testing.T) {
		cur := m2
		for i := 0; i < 3; i++ {
			u, _ := cur.Update(tea.KeyPressMsg{Code: tea.KeyRight})
			cur = u.(recommendationViewModel)
		}
		if cur.activeTab != 0 {
			t.Errorf("activeTab after 3 rights = %d, want 0", cur.activeTab)
		}
	})

	t.Run("left wraps around", func(t *testing.T) {
		u, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
		cur := u.(recommendationViewModel)
		if cur.activeTab != 2 {
			t.Errorf("activeTab after left from 0 = %d, want 2", cur.activeTab)
		}
	})

	t.Run("tab key navigates right", func(t *testing.T) {
		u, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		cur := u.(recommendationViewModel)
		if cur.activeTab != 1 {
			t.Errorf("activeTab after tab = %d, want 1", cur.activeTab)
		}
	})

	t.Run("shift+tab navigates left", func(t *testing.T) {
		u, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
		cur := u.(recommendationViewModel)
		if cur.activeTab != 2 {
			t.Errorf("activeTab after shift+tab from 0 = %d, want 2", cur.activeTab)
		}
	})

	t.Run("l navigates right", func(t *testing.T) {
		u, _ := m2.Update(tea.KeyPressMsg{Code: 'l', Text: "l"})
		cur := u.(recommendationViewModel)
		if cur.activeTab != 1 {
			t.Errorf("activeTab after l = %d, want 1", cur.activeTab)
		}
	})

	t.Run("h navigates left", func(t *testing.T) {
		u, _ := m2.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
		cur := u.(recommendationViewModel)
		if cur.activeTab != 2 {
			t.Errorf("activeTab after h from 0 = %d, want 2", cur.activeTab)
		}
	})
}

func TestRecommendationViewModelQuit(t *testing.T) {
	m2 := readyModel(t)

	t.Run("q quits", func(t *testing.T) {
		_, cmd := m2.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
		if cmd == nil {
			t.Error("expected quit command for q key")
		}
	})

	t.Run("esc quits", func(t *testing.T) {
		_, cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd == nil {
			t.Error("expected quit command for esc key")
		}
	})
}

func TestRecommendationViewModelViewBeforeReady(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	v := m.View()
	if !strings.Contains(v.Content, "Loading recommendation...") {
		t.Errorf("view before ready = %q, want Loading recommendation...", v.Content)
	}
	if !v.AltScreen {
		t.Error("pre-ready view should use alt screen")
	}
}

func TestRecommendationViewModelViewAfterReady(t *testing.T) {
	m2 := readyModel(t)
	v := m2.View()
	content := v.Content

	if !strings.Contains(content, "Recommendation L4-001") {
		t.Error("view missing recommendation header")
	}
	if !strings.Contains(content, "Kubernetes / Homolog") {
		t.Error("view missing service/environment subtitle")
	}
	if !strings.Contains(content, "quit") {
		t.Error("view missing footer help text")
	}
	if !v.AltScreen {
		t.Error("view should use alt screen")
	}
}

func TestRecommendationViewModelEmptyActions(t *testing.T) {
	data := map[string]interface{}{
		"recommendation_id": "L4-002",
		"service":           "EC2",
		"environment":       "production",
		"status":            "pending",
		"current_spending":  0.0,
		"monthly_savings":   0.0,
		"annual_savings":    0.0,
	}
	m := newRecommendationViewModel(data)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	if !strings.Contains(m2.tabs[1].content, "No content available") {
		t.Errorf("analysis tab should have placeholder, got %q", m2.tabs[1].content)
	}
	if !strings.Contains(m2.tabs[2].content, "No content available") {
		t.Errorf("implementation tab should have placeholder, got %q", m2.tabs[2].content)
	}
}

func TestRecommendationViewModelScrollDelegation(t *testing.T) {
	m2 := readyModel(t)

	longContent := strings.Repeat("line\n", 100)
	m2.viewport.SetContent(longContent)
	m2.tabs[0] = recommendationTab{title: "Overview", content: longContent}

	before := m2.viewport.ScrollPercent()
	u, _ := m2.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m3 := u.(recommendationViewModel)
	after := m3.viewport.ScrollPercent()

	if after <= before {
		t.Errorf("scroll percent did not increase: before=%f, after=%f", before, after)
	}
}

func TestNumberKeyTabJump(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m3 := u.(recommendationViewModel)
	if m3.activeTab != 2 {
		t.Errorf("activeTab after pressing 3 = %d, want 2", m3.activeTab)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: '1', Text: "1"})
	m4 := u.(recommendationViewModel)
	if m4.activeTab != 0 {
		t.Errorf("activeTab after pressing 1 = %d, want 0", m4.activeTab)
	}

	u, _ = m4.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	m5 := u.(recommendationViewModel)
	if m5.activeTab != 1 {
		t.Errorf("activeTab after pressing 2 = %d, want 1", m5.activeTab)
	}
}

func TestDynamicWordWrap(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())

	u120, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m120 := u120.(recommendationViewModel)

	u60, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 40})
	m60 := u60.(recommendationViewModel)

	if m120.tabs[1].content == m60.tabs[1].content {
		t.Error("markdown tabs should re-render at different widths")
	}
}

func TestTabPositionIndicator(t *testing.T) {
	m2 := readyModel(t)
	v := m2.View()
	if !strings.Contains(v.Content, "[1/3]") {
		t.Error("view should contain [1/3] position indicator")
	}

	u, _ := m2.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	m3 := u.(recommendationViewModel)
	v3 := m3.View()
	if !strings.Contains(v3.Content, "[3/3]") {
		t.Error("view should contain [3/3] after switching to tab 3")
	}
}

func TestStatusBadgeV2(t *testing.T) {
	badge := tuiStatusBadge("active")
	if badge == "" {
		t.Error("badge should not be empty")
	}
	if !strings.Contains(badge, "active") {
		t.Errorf("badge should contain status text, got %q", badge)
	}

	unknown := tuiStatusBadge("unknown_status")
	if !strings.Contains(unknown, "unknown_status") {
		t.Errorf("unknown status badge should contain text, got %q", unknown)
	}
}

func TestRelativeTime(t *testing.T) {
	old := relativeTime("2020-01-01")
	if !strings.Contains(old, "ago") || !strings.Contains(old, "Jan 1, 2020") {
		t.Errorf("relativeTime for old date = %q, want 'N days ago (Jan 1, 2020)'", old)
	}

	invalid := relativeTime("not-a-date")
	if invalid != "not-a-date" {
		t.Errorf("relativeTime for invalid = %q, want raw string", invalid)
	}
}

func TestSavingsBar(t *testing.T) {
	bar := savingsBar(30.0, 20)
	if !strings.Contains(bar, "█") {
		t.Error("savings bar should contain filled blocks")
	}
	if !strings.Contains(bar, "░") {
		t.Error("savings bar should contain empty blocks")
	}
	if !strings.Contains(bar, "30.0%") {
		t.Errorf("savings bar should contain percentage, got %q", bar)
	}

	zero := savingsBar(0, 20)
	if !strings.Contains(zero, "0.0%") {
		t.Errorf("zero savings bar = %q", zero)
	}
}

func TestClipboardCopy(t *testing.T) {
	m2 := readyModel(t)

	u, cmd := m2.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m3 := u.(recommendationViewModel)

	if cmd == nil {
		t.Error("expected command from clipboard copy")
	}
	if m3.feedback != feedbackCopied {
		t.Errorf("feedback = %q, want 'Copied to clipboard!'", m3.feedback)
	}
}

func TestClearFeedbackMsg(t *testing.T) {
	m2 := readyModel(t)
	m2.feedback = feedbackCopied

	u, _ := m2.Update(clearFeedbackMsg{})
	m3 := u.(recommendationViewModel)
	if m3.feedback != "" {
		t.Errorf("feedback should be cleared, got %q", m3.feedback)
	}
}

func TestOpenBrowserKey(t *testing.T) {
	origOpenBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origOpenBrowser }()

	m2 := readyModel(t)
	u, cmd := m2.Update(tea.KeyPressMsg{Code: 'o', Text: "o"})
	m3 := u.(recommendationViewModel)

	if openedURL == "" {
		t.Error("openBrowser should have been called")
	}
	if !strings.Contains(openedURL, "L4-001") {
		t.Errorf("opened URL should contain rec ID, got %q", openedURL)
	}
	if m3.feedback != "Opening in browser..." {
		t.Errorf("feedback = %q, want 'Opening in browser...'", m3.feedback)
	}
	if cmd == nil {
		t.Error("expected clearFeedback command")
	}
}

func TestSearchModeEntry(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m3 := u.(recommendationViewModel)

	if m3.mode != tuicommon.ModeSearch {
		t.Errorf("mode = %d, want tuicommon.ModeSearch (%d)", m3.mode, tuicommon.ModeSearch)
	}
}

func TestSearchModeEscClearsAndExits(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m3 := u.(recommendationViewModel)

	u, _ = m3.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m4 := u.(recommendationViewModel)

	if m4.mode != tuicommon.ModeNormal {
		t.Errorf("mode after esc = %d, want tuicommon.ModeNormal", m4.mode)
	}
	if m4.searchQuery != "" {
		t.Errorf("searchQuery should be empty after esc, got %q", m4.searchQuery)
	}
}

func TestSearchModeEnterCommits(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m3 := u.(recommendationViewModel)

	m3.searchInput.SetValue("Service")

	u, _ = m3.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m4 := u.(recommendationViewModel)

	if m4.mode != tuicommon.ModeNormal {
		t.Errorf("mode after enter = %d, want tuicommon.ModeNormal", m4.mode)
	}
	if m4.searchQuery != "Service" {
		t.Errorf("searchQuery = %q, want 'Service'", m4.searchQuery)
	}
}

func TestHelpModeToggle(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m3 := u.(recommendationViewModel)
	if m3.mode != tuicommon.ModeHelp {
		t.Errorf("mode after ? = %d, want tuicommon.ModeHelp", m3.mode)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m4 := u.(recommendationViewModel)
	if m4.mode != tuicommon.ModeNormal {
		t.Errorf("mode after second ? = %d, want tuicommon.ModeNormal", m4.mode)
	}
}

func TestHelpModeQuitExits(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m3 := u.(recommendationViewModel)

	_, cmd := m3.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Error("q in help mode should quit")
	}
}

func TestHelpOverlayInView(t *testing.T) {
	m2 := readyModel(t)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m3 := u.(recommendationViewModel)

	v := m3.View()
	if !strings.Contains(v.Content, "Keyboard Shortcuts") {
		t.Error("help overlay should show title")
	}
	if !strings.Contains(v.Content, "search") {
		t.Error("help overlay should list search binding")
	}
}

func TestFooterContextSearch(t *testing.T) {
	m2 := readyModel(t)

	v := m2.View()
	if !strings.Contains(v.Content, "search") {
		t.Error("normal footer should mention search")
	}

	u, _ := m2.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m3 := u.(recommendationViewModel)
	v3 := m3.View()
	if !strings.Contains(v3.Content, "confirm") {
		t.Error("search footer should mention confirm")
	}
}

func TestFooterFeedbackDisplay(t *testing.T) {
	m2 := readyModel(t)
	m2.feedback = feedbackCopied
	v := m2.View()
	if !strings.Contains(v.Content, "Copied") {
		t.Error("footer should show feedback message")
	}
}

func TestSavingsBarInSummaryTab(t *testing.T) {
	m2 := readyModel(t)
	content := m2.tabs[0].content
	if !strings.Contains(content, "█") {
		t.Error("summary tab should contain bar chart")
	}
}

func TestWordWrap(t *testing.T) {
	result := wordWrap("short text here for testing purposes only", 20)
	lines := strings.Split(result, "\n")
	for _, l := range lines {
		if len(l) > 20 {
			t.Errorf("line exceeds width: %q (len %d)", l, len(l))
		}
	}
}

func TestSearchHighlights(t *testing.T) {
	matches := searchHighlights("Hello World hello", "hello")
	if len(matches) != 2 {
		t.Errorf("expected 2 matches, got %d", len(matches))
	}

	empty := searchHighlights("content", "")
	if empty != nil {
		t.Error("empty query should return nil")
	}
}

func TestSearchHighlightsANSI(t *testing.T) {
	content := "\x1b[1mHello\x1b[0m World"
	matches := searchHighlights(content, "Hello")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
	start, end := matches[0][0], matches[0][1]
	got := content[start:end]
	if got != "Hello" {
		t.Errorf("matched slice = %q, want %q", got, "Hello")
	}

	matches2 := searchHighlights(content, "World")
	if len(matches2) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches2))
	}
	got2 := content[matches2[0][0]:matches2[0][1]]
	if got2 != "World" {
		t.Errorf("matched slice = %q, want %q", got2, "World")
	}
}

func TestSearchHighlightsNoMatch(t *testing.T) {
	matches := searchHighlights("Hello World", "xyz")
	if matches != nil {
		t.Errorf("expected nil for no matches, got %v", matches)
	}
}

func TestFormatDateFallback(t *testing.T) {
	got := formatDate("not-a-date")
	if got != "not-a-date" {
		t.Errorf("formatDate fallback = %q, want raw string", got)
	}
}

func TestRelativeTimeToday(t *testing.T) {
	now := time.Now().Format("2006-01-02T15:04:05Z")
	got := relativeTime(now)
	if !strings.Contains(got, "today") {
		t.Errorf("relativeTime for now = %q, want 'today (...)'", got)
	}
}

func TestRelativeTimeYesterday(t *testing.T) {
	yesterday := time.Now().Add(-30 * time.Hour).Format("2006-01-02T15:04:05Z")
	got := relativeTime(yesterday)
	if !strings.Contains(got, "yesterday") {
		t.Errorf("relativeTime for yesterday = %q, want 'yesterday (...)'", got)
	}
}

func TestRenderMarkdownContentSmallWidth(t *testing.T) {
	out := renderMarkdownContent("**bold**", 5)
	if out == "" {
		t.Error("renderMarkdownContent should return content for small width")
	}
}

func TestSavingsBarEdgeCases(t *testing.T) {
	small := savingsBar(50, 2)
	if !strings.Contains(small, "50.0%") {
		t.Errorf("small bar = %q", small)
	}

	large := savingsBar(50, 50)
	if !strings.Contains(large, "50.0%") {
		t.Errorf("large bar = %q", large)
	}

	over := savingsBar(150, 20)
	if !strings.Contains(over, "150.0%") {
		t.Errorf("over 100%% bar = %q", over)
	}
}

func TestWordWrapEdgeCases(t *testing.T) {
	got := wordWrap("hello", 0)
	if got != "hello" {
		t.Errorf("wordWrap with width=0 = %q, want raw string", got)
	}

	got2 := wordWrap("", 20)
	if got2 != "" {
		t.Errorf("wordWrap with empty = %q", got2)
	}
}

func TestInit(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return spinner tick command")
	}
}

func TestSwitchTabOutOfRange(t *testing.T) {
	m := readyModel(t)
	m.switchTab(-1)
	if m.activeTab != 0 {
		t.Errorf("switchTab(-1) changed activeTab to %d", m.activeTab)
	}
	m.switchTab(99)
	if m.activeTab != 0 {
		t.Errorf("switchTab(99) changed activeTab to %d", m.activeTab)
	}
}

func TestHelpModeSwallowsKeys(t *testing.T) {
	m := readyModel(t)

	u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m2 := u.(recommendationViewModel)

	u, _ = m2.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m3 := u.(recommendationViewModel)
	if m3.mode != tuicommon.ModeHelp {
		t.Errorf("random key in help mode changed mode to %d", m3.mode)
	}
}

func TestHelpModeEscExits(t *testing.T) {
	m := readyModel(t)

	u, _ := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	m2 := u.(recommendationViewModel)

	u, cmd := m2.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m3 := u.(recommendationViewModel)
	if m3.mode != tuicommon.ModeNormal {
		t.Errorf("esc in help mode should return to normal, got %d", m3.mode)
	}
	if cmd != nil {
		t.Error("esc in help mode should not quit")
	}
}

func TestSearchModeTyping(t *testing.T) {
	m := readyModel(t)

	u, _ := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	m2 := u.(recommendationViewModel)

	u, _ = m2.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m3 := u.(recommendationViewModel)
	if m3.mode != tuicommon.ModeSearch {
		t.Errorf("typing should stay in search mode, got %d", m3.mode)
	}
}

func TestNextPrevMatchWithQuery(t *testing.T) {
	m := readyModel(t)
	m.searchQuery = "Service"
	m.applySearchHighlights()

	u, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m2 := u.(recommendationViewModel)
	_ = m2

	u, _ = m2.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
	_ = u.(recommendationViewModel)
}

func TestNextPrevMatchWithoutQuery(t *testing.T) {
	m := readyModel(t)

	u, _ := m.Update(tea.KeyPressMsg{Code: 'n', Text: "n"})
	m2 := u.(recommendationViewModel)
	if m2.activeTab != m.activeTab {
		t.Error("n without query should not change state")
	}

	u, _ = m2.Update(tea.KeyPressMsg{Code: 'N', Text: "N"})
	m3 := u.(recommendationViewModel)
	if m3.activeTab != m.activeTab {
		t.Error("N without query should not change state")
	}
}

func TestUpdateSmallWindow(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())

	u, _ := m.Update(tea.WindowSizeMsg{Width: 10, Height: 3})
	m2 := u.(recommendationViewModel)
	if !m2.ready {
		t.Error("small window should still set ready")
	}
}

func TestClipboardCopyFallbackToContent(t *testing.T) {
	m := readyModel(t)
	m.switchTab(0)

	u, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m2 := u.(recommendationViewModel)
	if cmd == nil {
		t.Error("expected command from clipboard copy")
	}
	if m2.feedback != feedbackCopied {
		t.Errorf("feedback = %q", m2.feedback)
	}
}

func TestRightAlign(t *testing.T) {
	got := rightAlign("left", "right", 5)
	if !strings.Contains(got, "left") || !strings.Contains(got, "right") {
		t.Errorf("rightAlign short = %q", got)
	}
}

func TestHelpOverlaySmallHeight(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	u, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 6})
	m2 := u.(recommendationViewModel)
	m2.mode = tuicommon.ModeHelp
	v := m2.View()
	if !strings.Contains(v.Content, "Keyboard Shortcuts") {
		t.Error("help overlay should render even with small height")
	}
}

func TestClearFeedbackReturnsCmd(t *testing.T) {
	cmd := clearFeedback()
	if cmd == nil {
		t.Error("clearFeedback should return a tick command")
	}
}

func TestClearFeedbackTick(t *testing.T) {
	msg := clearFeedbackTick(time.Time{})
	if _, ok := msg.(clearFeedbackMsg); !ok {
		t.Errorf("clearFeedbackTick returned %T, want clearFeedbackMsg", msg)
	}
}

func TestDrainTermInput(t *testing.T) {
	tuicommon.DrainTermInput()
}

func TestStartTUI(t *testing.T) {
	r, w, _ := os.Pipe()
	w.Write([]byte("q"))
	w.Close()
	var out bytes.Buffer
	err := startTUI(fullRecommendationData(), tea.WithInput(r), tea.WithOutput(&out))
	if err != nil {
		t.Fatalf("startTUI error: %v", err)
	}
}

func TestUpdateBeforeReady(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	u, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m2 := u.(recommendationViewModel)
	if m2.ready {
		t.Error("should not be ready")
	}
	if cmd != nil {
		t.Error("should return nil cmd before ready")
	}
}

func TestSpinnerTickBeforeReady(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	msg := m.spinner.Tick()
	u, cmd := m.Update(msg)
	m2 := u.(recommendationViewModel)
	if m2.ready {
		t.Error("should not be ready after spinner tick")
	}
	if cmd == nil {
		t.Error("spinner tick before ready should return next tick command")
	}
}

func TestSpinnerTickAfterReady(t *testing.T) {
	m := readyModel(t)
	msg := spinner.TickMsg{}
	_, cmd := m.Update(msg)
	if cmd != nil {
		t.Error("spinner tick after ready should return nil cmd")
	}
}

func TestApplySearchHighlightsWithMatches(t *testing.T) {
	m := readyModel(t)
	m.searchQuery = "Service"
	m.applySearchHighlights()
	if m.matchCount == 0 {
		t.Error("expected matches for 'Service' in overview tab")
	}
}

func TestApplySearchHighlightsNoMatches(t *testing.T) {
	m := readyModel(t)
	m.searchQuery = "zzzznonexistent"
	m.applySearchHighlights()
	if m.matchCount != 0 {
		t.Errorf("expected 0 matches, got %d", m.matchCount)
	}
}

func TestHelpOverlayPadding(t *testing.T) {
	m := readyModel(t)
	m.mode = tuicommon.ModeHelp
	out := renderHelpOverlay(m)
	if out == "" {
		t.Error("help overlay should not be empty")
	}
	lines := strings.Count(out, "\n")
	expectedHeight := m.height - 4 - 1
	if lines < expectedHeight-1 {
		t.Errorf("help overlay should be padded to fill viewport, got %d lines, want ~%d", lines, expectedHeight)
	}
}

func TestHelpOverlayTinyHeight(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	u, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 4})
	m2 := u.(recommendationViewModel)
	m2.mode = tuicommon.ModeHelp
	out := renderHelpOverlay(m2)
	if !strings.Contains(out, "Keyboard Shortcuts") {
		t.Error("help overlay should render even with tiny height")
	}
}

func TestToFloat(t *testing.T) {
	if toFloat(42.5) != 42.5 {
		t.Error("float64 case failed")
	}
	if toFloat(10) != 10.0 {
		t.Error("int case failed")
	}
	if toFloat("nope") != 0 {
		t.Error("default case should return 0")
	}
}

func TestParseCodeBlocks(t *testing.T) {
	raw := "# Title\n\nSome text\n\n```hcl\nresource \"aws_instance\" {}\n```\n\nMore text\n\n```bash\nterraform apply\n```"
	blocks := parseCodeBlocks(raw)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 code blocks, got %d", len(blocks))
	}
	if blocks[0].lang != "hcl" {
		t.Errorf("block[0].lang = %q, want hcl", blocks[0].lang)
	}
	if blocks[0].code != "resource \"aws_instance\" {}" {
		t.Errorf("block[0].code = %q", blocks[0].code)
	}
	if blocks[1].lang != "bash" {
		t.Errorf("block[1].lang = %q, want bash", blocks[1].lang)
	}
	if blocks[1].code != "terraform apply" {
		t.Errorf("block[1].code = %q", blocks[1].code)
	}
}

func TestParseCodeBlocksEmpty(t *testing.T) {
	blocks := parseCodeBlocks("no code here")
	if len(blocks) != 0 {
		t.Errorf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestParseCodeBlocksNoLang(t *testing.T) {
	raw := "```\nplain code\n```"
	blocks := parseCodeBlocks(raw)
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block, got %d", len(blocks))
	}
	if blocks[0].lang != "" {
		t.Errorf("block.lang = %q, want empty", blocks[0].lang)
	}
	if blocks[0].code != "plain code" {
		t.Errorf("block.code = %q", blocks[0].code)
	}
}

func TestMergeImplementationRaw(t *testing.T) {
	t.Run("both present", func(t *testing.T) {
		got := mergeImplementationRaw("impl", "exec")
		if !strings.Contains(got, "impl") || !strings.Contains(got, "exec") || !strings.Contains(got, "---") {
			t.Errorf("merged = %q", got)
		}
	})
	t.Run("impl only", func(t *testing.T) {
		if mergeImplementationRaw("impl", "") != "impl" {
			t.Error("should return impl when exec is empty")
		}
	})
	t.Run("exec only", func(t *testing.T) {
		if mergeImplementationRaw("", "exec") != "exec" {
			t.Error("should return exec when impl is empty")
		}
	})
	t.Run("both empty", func(t *testing.T) {
		if mergeImplementationRaw("", "") != "" {
			t.Error("should return empty when both empty")
		}
	})
}

func codeBlockData() map[string]interface{} {
	return map[string]interface{}{
		"recommendation_id":  "L4-CB",
		"service":            "EC2",
		"environment":        "production",
		"status":             "active",
		"current_spending":   100.0,
		"monthly_savings":    50.0,
		"annual_savings":     600.0,
		"savings_percentage": 50.0,
		"actions": map[string]interface{}{
			"description":            "Rightsize instance",
			"key_takeaway":           "**Risk Level:** Low",
			"implementation_process": "## Steps\n\n1. Change instance type\n2. Apply changes",
			"execution_method":       "### Terraform\n\n```hcl\nresource \"aws_instance\" \"web\" {\n  instance_type = \"t3.medium\"\n}\n```\n\n### CLI\n\n```bash\naws ec2 modify-instance-attribute --instance-type t3.medium\n```",
		},
	}
}

func TestCodeBlocksInImplementationTab(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	implTab := m2.tabs[2]
	if len(implTab.codeBlocks) != 2 {
		t.Fatalf("expected 2 code blocks in implementation tab, got %d", len(implTab.codeBlocks))
	}
	if implTab.codeBlocks[0].lang != "hcl" {
		t.Errorf("block[0].lang = %q, want hcl", implTab.codeBlocks[0].lang)
	}
	if implTab.codeBlocks[1].lang != "bash" {
		t.Errorf("block[1].lang = %q, want bash", implTab.codeBlocks[1].lang)
	}
}

func TestCodeBlockBordersInContent(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	content := m2.tabs[2].content
	if !strings.Contains(content, "┌") || !strings.Contains(content, "┐") {
		t.Error("implementation tab should contain top border")
	}
	if !strings.Contains(content, "└") || !strings.Contains(content, "┘") {
		t.Error("implementation tab should contain bottom border")
	}
	if !strings.Contains(content, "hcl") {
		t.Error("border should contain language label 'hcl'")
	}
	if !strings.Contains(content, "bash") {
		t.Error("border should contain language label 'bash'")
	}
}

func TestCodeBlockNavigation(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	if m2.activeBlock != -1 {
		t.Errorf("activeBlock should be -1 initially, got %d", m2.activeBlock)
	}

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock != 0 {
		t.Errorf("activeBlock after ] = %d, want 0", m3.activeBlock)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m4 := u.(recommendationViewModel)
	if m4.activeBlock != 1 {
		t.Errorf("activeBlock after second ] = %d, want 1", m4.activeBlock)
	}

	u, _ = m4.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m5 := u.(recommendationViewModel)
	if m5.activeBlock != 0 {
		t.Errorf("activeBlock should wrap to 0, got %d", m5.activeBlock)
	}
}

func TestCodeBlockNavigationPrev(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock != 1 {
		t.Errorf("activeBlock after [ from -1 = %d, want 1 (wrap)", m3.activeBlock)
	}
}

func TestCodeBlockCopy(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)

	u, cmd := m3.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m4 := u.(recommendationViewModel)
	if cmd == nil {
		t.Error("expected command from code block copy")
	}
	wantFeedback := "Copied hcl block (1/2)"
	if m4.feedback != wantFeedback {
		t.Errorf("feedback = %q, want %q", m4.feedback, wantFeedback)
	}
}

func TestCodeBlockCopyFallbackNoBlocks(t *testing.T) {
	m := readyModel(t)
	m.switchTab(0)

	u, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m2 := u.(recommendationViewModel)
	if cmd == nil {
		t.Error("expected command from copy")
	}
	if m2.feedback != feedbackCopied {
		t.Errorf("feedback = %q", m2.feedback)
	}
}

func TestCopyAllKey(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, cmd := m2.Update(tea.KeyPressMsg{Code: 'C', Text: "C"})
	m3 := u.(recommendationViewModel)
	if cmd == nil {
		t.Error("expected command from copy all")
	}
	if m3.feedback != feedbackCopied {
		t.Errorf("feedback = %q", m3.feedback)
	}
}

func TestCodeBlockNavigationNoBlocksTab(t *testing.T) {
	m := readyModel(t)
	m.switchTab(0)

	u, _ := m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m2 := u.(recommendationViewModel)
	if m2.activeBlock != -1 {
		t.Errorf("activeBlock should stay -1 on tab with no blocks, got %d", m2.activeBlock)
	}
}

func TestSwitchTabResetsActiveBlock(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock < 0 {
		t.Fatal("expected activeBlock >= 0 after navigation")
	}

	m3.switchTab(0)
	if m3.activeBlock != -1 {
		t.Errorf("activeBlock should reset to -1 on tab switch, got %d", m3.activeBlock)
	}
}

func TestSummaryTabContent(t *testing.T) {
	m := readyModel(t)
	content := m.tabs[0].content
	if !strings.Contains(content, "Kubernetes") {
		t.Error("summary tab should contain service name")
	}
	if !strings.Contains(content, "Homolog") {
		t.Error("summary tab should contain environment")
	}
	if !strings.Contains(content, "$500.00/mo") {
		t.Error("summary tab should contain current spending")
	}
	if !strings.Contains(content, "$150.00/mo") {
		t.Error("summary tab should contain monthly savings")
	}
	if !strings.Contains(content, "$1800.00/yr") {
		t.Error("summary tab should contain annual savings")
	}
	if !strings.Contains(content, "█") {
		t.Error("summary tab should contain savings bar")
	}
}

func TestFooterBlockIndicator(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	v := m3.View()
	if !strings.Contains(v.Content, "Block 1/2") {
		t.Error("footer should show block indicator when block is selected")
	}
}

func TestHasCodeBlocks(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	m2.activeTab = 0
	if m2.hasCodeBlocks() {
		t.Error("summary tab should not have code blocks")
	}
	m2.activeTab = 2
	if !m2.hasCodeBlocks() {
		t.Error("implementation tab should have code blocks")
	}
}

func TestAddCodeBlockBordersNone(t *testing.T) {
	rendered := "plain text\nno code here"
	result, blocks := addCodeBlockBorders(rendered, nil, 80, -1)
	if result != rendered {
		t.Error("should return unchanged content when no blocks")
	}
	if len(blocks) != 0 {
		t.Error("should return empty blocks")
	}
}

func TestCodeBlockNavigationPrevFromZero(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock != 0 {
		t.Fatalf("expected block 0 after ], got %d", m3.activeBlock)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m4 := u.(recommendationViewModel)
	if m4.activeBlock != 1 {
		t.Errorf("activeBlock after [ from 0 = %d, want 1 (wrap to last)", m4.activeBlock)
	}
}

func TestCodeBlockSingleBlock(t *testing.T) {
	data := map[string]interface{}{
		"recommendation_id":  "L4-SB",
		"service":            "EC2",
		"environment":        "prod",
		"status":             "active",
		"current_spending":   100.0,
		"monthly_savings":    50.0,
		"annual_savings":     600.0,
		"savings_percentage": 50.0,
		"actions": map[string]interface{}{
			"description":            "test",
			"key_takeaway":           "low risk",
			"implementation_process": "step one",
			"execution_method":       "```hcl\nresource {}\n```",
		},
	}
	m := newRecommendationViewModel(data)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock != 0 {
		t.Errorf("single block: ] should go to 0, got %d", m3.activeBlock)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m4 := u.(recommendationViewModel)
	if m4.activeBlock != 0 {
		t.Errorf("single block: ] should stay at 0, got %d", m4.activeBlock)
	}

	u, _ = m4.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m5 := u.(recommendationViewModel)
	if m5.activeBlock != 0 {
		t.Errorf("single block: [ should stay at 0, got %d", m5.activeBlock)
	}
}

func TestReadyViewModelStartsReady(t *testing.T) {
	m := newReadyViewModel(fullRecommendationData(), 120, 40)

	if !m.ready {
		t.Fatal("newReadyViewModel should start ready")
	}
	if len(m.tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(m.tabs))
	}

	v := m.View()
	if !strings.Contains(v.Content, "Recommendation L4-001") {
		t.Error("should show recommendation header")
	}
	if strings.Contains(v.Content, "Loading recommendation") {
		t.Error("should NOT show loading spinner")
	}
}

func TestReadyViewModelTUIProgram(t *testing.T) {
	m := newReadyViewModel(fullRecommendationData(), 120, 40)

	r, w, _ := os.Pipe()
	var out bytes.Buffer

	go func() {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte("q"))
		w.Close()
	}()

	_, err := tea.NewProgram(m,
		tea.WithInput(r),
		tea.WithOutput(&out),
		tea.WithWindowSize(120, 40),
	).Run()
	if err != nil {
		t.Fatalf("TUI error: %v", err)
	}

	rendered := out.String()
	if strings.Contains(rendered, "Loading recommendation") {
		t.Fatal("TUI should never show loading when started with newReadyViewModel")
	}
}

func TestParseCodeBlocksEmptyBody(t *testing.T) {
	raw := "```hcl\n```"
	blocks := parseCodeBlocks(raw)
	if len(blocks) != 0 {
		t.Errorf("empty-body code block should not be parsed, got %d blocks", len(blocks))
	}
}

func TestFirstNonEmptyCodeLine(t *testing.T) {
	if got := firstNonEmptyCodeLine(codeBlock{code: "hello"}); got != "hello" {
		t.Errorf("got %q", got)
	}
	if got := firstNonEmptyCodeLine(codeBlock{code: "\n\nhello"}); got != "hello" {
		t.Errorf("got %q for leading blanks", got)
	}
	if got := firstNonEmptyCodeLine(codeBlock{code: "  \n  "}); got != "" {
		t.Errorf("got %q for whitespace-only", got)
	}
}

func TestRenderBlockBorder(t *testing.T) {
	top, bottom := renderBlockBorder("hcl", 40, false)
	if !strings.Contains(top, "hcl") || !strings.Contains(top, "┌") {
		t.Errorf("top = %q", top)
	}
	if !strings.Contains(bottom, "└") || !strings.Contains(bottom, "┘") {
		t.Errorf("bottom = %q", bottom)
	}

	top2, _ := renderBlockBorder("", 40, false)
	if !strings.Contains(top2, "code") {
		t.Error("empty lang should default to 'code'")
	}

	top3, _ := renderBlockBorder("verylonglanguagename", 10, false)
	if !strings.Contains(top3, "verylonglanguagename") {
		t.Error("narrow width should still show lang")
	}
}

func TestFilterTerminalNoise(t *testing.T) {
	noise := tea.KeyPressMsg{Code: '/', Text: "/1919/2020"}
	if tuicommon.FilterTerminalNoise(nil, noise) != nil {
		t.Error("should filter OSC noise")
	}

	normal := tea.KeyPressMsg{Code: 'q', Text: "q"}
	if tuicommon.FilterTerminalNoise(nil, normal) == nil {
		t.Error("should pass normal keys")
	}

	resize := tea.WindowSizeMsg{Width: 80, Height: 24}
	if tuicommon.FilterTerminalNoise(nil, resize) == nil {
		t.Error("should pass non-key messages")
	}
}

func TestNewReadyViewModelSmall(t *testing.T) {
	m := newReadyViewModel(fullRecommendationData(), 30, 6)
	if !m.ready {
		t.Fatal("should be ready")
	}
	if len(m.tabs) != 3 {
		t.Fatalf("expected 3 tabs, got %d", len(m.tabs))
	}
}

func TestHasCodeBlocksNoTabs(t *testing.T) {
	m := newRecommendationViewModel(fullRecommendationData())
	if m.hasCodeBlocks() {
		t.Error("should return false when tabs not built")
	}
}

func TestNavigateBlockEmpty(t *testing.T) {
	m := readyModel(t)
	m.switchTab(0)
	m.navigateBlock(1)
	if m.activeBlock != -1 {
		t.Errorf("navigateBlock on tab with no blocks should not change activeBlock, got %d", m.activeBlock)
	}
}

func TestCopyContentAllOnBlockTab(t *testing.T) {
	m := newReadyViewModel(codeBlockData(), 120, 40)
	m.switchTab(2)
	m.activeBlock = 0

	cmd := m.copyContent(true)
	if cmd == nil {
		t.Error("copyContent(true) should return command")
	}
	if m.feedback != feedbackCopied {
		t.Errorf("feedback = %q", m.feedback)
	}
}

func TestAddCodeBlockBordersEmptyCode(t *testing.T) {
	blocks := []codeBlock{{lang: "hcl", code: "  \n  "}}
	result, _ := addCodeBlockBorders("some rendered\ncontent", blocks, 80, -1)
	if !strings.Contains(result, "some rendered") {
		t.Error("should preserve content when block has empty code")
	}
}

func TestAddCodeBlockBordersEndLineClamped(t *testing.T) {
	rendered := "line1\nresource {}\nline3"
	blocks := []codeBlock{{lang: "hcl", code: "resource {}\nline2\nline3\nline4\nline5"}}
	result, _ := addCodeBlockBorders(rendered, blocks, 40, -1)
	if !strings.Contains(result, "┌") {
		t.Error("should add top border")
	}
}

func TestAddCodeBlockBordersNoMatch(t *testing.T) {
	blocks := []codeBlock{{lang: "hcl", code: "unique_code_not_in_rendered"}}
	result, _ := addCodeBlockBorders("totally different content", blocks, 80, -1)
	if strings.Contains(result, "┌") {
		t.Error("should not add borders when no match found")
	}
}

func TestNewReadyViewModelTinyViewport(t *testing.T) {
	m := newReadyViewModel(fullRecommendationData(), 15, 4)
	if !m.ready {
		t.Fatal("should be ready even with tiny viewport")
	}
}

func TestNavigateBlockDecrement(t *testing.T) {
	m := newReadyViewModel(codeBlockData(), 120, 40)
	m.switchTab(2)
	m.activeBlock = 1
	m.navigateBlock(-1)
	if m.activeBlock != 0 {
		t.Errorf("activeBlock = %d, want 0", m.activeBlock)
	}
}

func TestCopyContentBlockNegativeIndex(t *testing.T) {
	m := newReadyViewModel(codeBlockData(), 120, 40)
	m.switchTab(2)
	m.activeBlock = -1
	cmd := m.copyContent(false)
	if cmd == nil {
		t.Error("should return command")
	}
	if m.activeBlock != 0 {
		t.Errorf("activeBlock should be set to 0, got %d", m.activeBlock)
	}
}

func TestInitReadyModel(t *testing.T) {
	m := newReadyViewModel(fullRecommendationData(), 120, 40)
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init should return nil when already ready")
	}
}

func TestActiveBlockHighlight(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	content := m2.tabs[2].content
	if !strings.Contains(content, "┌") {
		t.Fatal("inactive blocks should use ┌ border")
	}

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)

	content = m3.tabs[2].content
	if !strings.Contains(content, "▶") {
		t.Error("active block should use ▶ indicator")
	}
	if !strings.Contains(content, "┌") {
		t.Error("inactive blocks should still use ┌ border")
	}
}

func TestScrollWithContext(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)

	blocks := m3.tabs[m3.activeTab].codeBlocks
	if len(blocks) == 0 {
		t.Fatal("expected code blocks")
	}
	startLine := blocks[0].startLine
	if startLine < 5 {
		t.Skip("first block too close to top to test context offset")
	}
	expectedOffset := startLine - 5
	if m3.viewport.YOffset() != expectedOffset {
		t.Errorf("viewport offset = %d, want %d (startLine %d minus 2)", m3.viewport.YOffset(), expectedOffset, startLine)
	}
}

func TestWrapFeedbackForward(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	u, _ = m3.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m4 := u.(recommendationViewModel)

	if m4.feedback != "" {
		t.Errorf("should not show wrap feedback before wrapping, got %q", m4.feedback)
	}

	u, _ = m4.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m5 := u.(recommendationViewModel)
	if m5.activeBlock != 0 {
		t.Fatalf("expected wrap to block 0, got %d", m5.activeBlock)
	}
	if m5.feedback != "↻ Wrapped to first block" {
		t.Errorf("feedback = %q, want wrap message", m5.feedback)
	}
}

func TestWrapFeedbackBackward(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.activeBlock != 0 {
		t.Fatalf("expected block 0, got %d", m3.activeBlock)
	}

	u, _ = m3.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m4 := u.(recommendationViewModel)
	if m4.activeBlock != 1 {
		t.Fatalf("expected wrap to block 1, got %d", m4.activeBlock)
	}
	if m4.feedback != "↻ Wrapped to last block" {
		t.Errorf("feedback = %q, want wrap message", m4.feedback)
	}
}

func TestNoWrapFeedbackFromInitial(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: '[', Text: "["})
	m3 := u.(recommendationViewModel)
	if m3.feedback != "" {
		t.Errorf("initial [ from -1 should not show wrap feedback, got %q", m3.feedback)
	}
}

func TestCopyFeedbackWithBlockInfo(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(2)

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	u, _ = m3.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m4 := u.(recommendationViewModel)

	u, _ = m4.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	m5 := u.(recommendationViewModel)
	if m5.feedback != "Copied bash block (2/2)" {
		t.Errorf("feedback = %q, want block-specific message", m5.feedback)
	}
}

func TestRenderBlockBorderActive(t *testing.T) {
	top, bottom := renderBlockBorder("hcl", 40, true)
	if !strings.Contains(top, "▶") {
		t.Error("active top border should contain ▶")
	}
	if !strings.Contains(top, "hcl") {
		t.Error("active top border should contain language")
	}
	if !strings.Contains(bottom, "└") || !strings.Contains(bottom, "┘") {
		t.Error("active bottom border should have corners")
	}

	topInactive, _ := renderBlockBorder("hcl", 40, false)
	if strings.Contains(topInactive, "▶") {
		t.Error("inactive top border should not contain ▶")
	}
	if !strings.Contains(topInactive, "┌") {
		t.Error("inactive top border should contain ┌")
	}
}

func TestRefreshBlockHighlightNoBlocks(t *testing.T) {
	m := readyModel(t)
	m.switchTab(0)
	m.refreshBlockHighlight()
	if m.activeBlock != -1 {
		t.Errorf("refreshBlockHighlight on tab with no blocks should not change activeBlock")
	}
}

func TestTabRenderedFieldPopulated(t *testing.T) {
	m := newRecommendationViewModel(codeBlockData())
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m2 := updated.(recommendationViewModel)

	if m2.tabs[2].rendered == "" {
		t.Error("implementation tab should have rendered field populated")
	}
	if m2.tabs[0].rendered != "" {
		t.Error("summary tab should not have rendered field")
	}
}

func TestRenderBlockBorderActiveNarrow(t *testing.T) {
	top, _ := renderBlockBorder("verylonglanguagename", 10, true)
	if !strings.Contains(top, "▶") {
		t.Error("narrow active border should still contain ▶")
	}
	if !strings.Contains(top, "verylonglanguagename") {
		t.Error("narrow active border should still contain lang")
	}
}

func TestRefreshBlockHighlightNarrowWidth(t *testing.T) {
	m := newReadyViewModel(codeBlockData(), 22, 20)
	m.switchTab(2)
	m.activeBlock = 0
	m.refreshBlockHighlight()
	if m.tabs[2].content == "" {
		t.Error("should produce content even at narrow width")
	}
}

func TestNavigateBlockOffsetClampedToZero(t *testing.T) {
	data := map[string]interface{}{
		"recommendation_id":  "L4-TOP",
		"service":            "EC2",
		"environment":        "prod",
		"status":             "active",
		"current_spending":   100.0,
		"monthly_savings":    50.0,
		"annual_savings":     600.0,
		"savings_percentage": 50.0,
		"actions": map[string]interface{}{
			"description":            "test",
			"key_takeaway":           "```hcl\nresource {}\n```",
			"implementation_process": "step",
			"execution_method":       "",
		},
	}
	m := newRecommendationViewModel(data)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m2 := updated.(recommendationViewModel)
	m2.switchTab(1)

	blocks := m2.tabs[1].codeBlocks
	if len(blocks) == 0 {
		t.Fatal("expected code blocks on analysis tab")
	}

	u, _ := m2.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m3 := u.(recommendationViewModel)
	if m3.viewport.YOffset() < 0 {
		t.Error("viewport offset should never be negative")
	}
}

func TestCopyBlockNoLang(t *testing.T) {
	data := map[string]interface{}{
		"recommendation_id":  "L4-NL",
		"service":            "EC2",
		"environment":        "prod",
		"status":             "active",
		"current_spending":   100.0,
		"monthly_savings":    50.0,
		"annual_savings":     600.0,
		"savings_percentage": 50.0,
		"actions": map[string]interface{}{
			"description":            "test",
			"key_takeaway":           "low risk",
			"implementation_process": "step",
			"execution_method":       "```\nsome code\n```",
		},
	}
	m := newReadyViewModel(data, 120, 40)
	m.switchTab(2)

	u, _ := m.Update(tea.KeyPressMsg{Code: ']', Text: "]"})
	m2 := u.(recommendationViewModel)
	cmd := m2.copyContent(false)
	if cmd == nil {
		t.Error("should return command")
	}
	if m2.feedback != "Copied code block (1/1)" {
		t.Errorf("feedback = %q, want code fallback label", m2.feedback)
	}
}
