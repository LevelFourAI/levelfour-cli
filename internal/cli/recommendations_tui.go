package cli

import (
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/x/ansi"

	"golang.org/x/term"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}

const feedbackCopied = "Copied to clipboard!"

type tuiKeyMap struct {
	Tabs      key.Binding
	Left      key.Binding
	Right     key.Binding
	Up        key.Binding
	Down      key.Binding
	Search    key.Binding
	Copy      key.Binding
	CopyAll   key.Binding
	NextBlock key.Binding
	PrevBlock key.Binding
	Open      key.Binding
	Help      key.Binding
	Quit      key.Binding
	Enter     key.Binding
	NextMatch key.Binding
	PrevMatch key.Binding
	Cancel    key.Binding
	CloseHelp key.Binding
}

var keys = tuiKeyMap{
	Tabs:      key.NewBinding(key.WithKeys("1", "2", "3"), key.WithHelp("1-3", "jump to tab")),
	Left:      key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev tab")),
	Right:     key.NewBinding(key.WithKeys("right", "l", "tab"), key.WithHelp("→/l", "next tab")),
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "scroll up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "scroll down")),
	Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Copy:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "copy block")),
	CopyAll:   key.NewBinding(key.WithKeys("C"), key.WithHelp("C", "copy all")),
	NextBlock: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next block")),
	PrevBlock: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "prev block")),
	Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:      key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
	NextMatch: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
	PrevMatch: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	CloseHelp: key.NewBinding(key.WithKeys("?", "esc"), key.WithHelp("?/esc", "close")),
}

type codeBlock struct {
	lang      string
	code      string
	startLine int
}

type recommendationTab struct {
	title      string
	raw        string
	rendered   string
	content    string
	codeBlocks []codeBlock
}

type recommendationViewModel struct {
	tabs      []recommendationTab
	activeTab int
	viewport  viewport.Model
	width     int
	height    int
	ready     bool

	recID          string
	service        string
	environment    string
	status         string
	createdAt      string
	analysisPeriod string
	description    string

	currentSpending float64
	monthlySavings  float64
	annualSavings   float64
	pct             float64

	rawKeyTakeaway    string
	rawImplementation string
	rawExecution      string

	activeBlock int

	mode        tuicommon.Mode
	searchInput textinput.Model
	searchQuery string
	matchCount  int

	feedback string

	help    help.Model
	spinner spinner.Model
}

type clearFeedbackMsg struct{}

func clearFeedbackTick(time.Time) tea.Msg {
	return clearFeedbackMsg{}
}

func clearFeedback() tea.Cmd {
	return tea.Tick(1500*time.Millisecond, clearFeedbackTick)
}

var dateLayouts = []string{
	time.RFC3339,
	"2006-01-02T15:04:05.999999",
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

func parseDate(raw string) (time.Time, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func formatDate(raw string) string {
	if t, ok := parseDate(raw); ok {
		return t.Format("Jan 2, 2006")
	}
	return raw
}

func relativeTime(raw string) string {
	t, ok := parseDate(raw)
	if !ok {
		return raw
	}
	d := time.Since(t)
	formatted := t.Format("Jan 2, 2006")
	switch {
	case d < 24*time.Hour:
		return fmt.Sprintf("today (%s)", formatted)
	case d < 48*time.Hour:
		return fmt.Sprintf("yesterday (%s)", formatted)
	default:
		days := int(d.Hours() / 24)
		return fmt.Sprintf("%d days ago (%s)", days, formatted)
	}
}

func renderMarkdownContent(md string, width int) string {
	if width < 20 {
		width = 20
	}
	r, _ := glamour.NewTermRenderer(glamour.WithStandardStyle(styles.DarkStyle), glamour.WithWordWrap(width))
	out, _ := r.Render(md)
	return out
}

var codeBlockRegex = regexp.MustCompile("(?m)^```(\\w*)\\n([\\s\\S]*?)\\n```(?:$|\\n)")

func parseCodeBlocks(raw string) []codeBlock {
	matches := codeBlockRegex.FindAllStringSubmatch(raw, -1)
	blocks := make([]codeBlock, len(matches))
	for i, m := range matches {
		blocks[i] = codeBlock{lang: m[1], code: m[2]}
	}
	return blocks
}

type borderInsertion struct {
	lineIdx int
	text    string
	isAbove bool
}

func firstNonEmptyCodeLine(block codeBlock) string {
	codeLines := strings.Split(block.code, "\n")
	for _, line := range codeLines {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

func renderBlockBorder(lang string, width int, active bool) (string, string) {
	if lang == "" {
		lang = "code"
	}
	remainingDashes := width - len(lang) - 5
	if remainingDashes < 1 {
		remainingDashes = 1
	}
	if active {
		style := lipglossv2.NewStyle().Foreground(output.BrandAccent).Bold(true)
		dashes := remainingDashes - 2
		if dashes < 1 {
			dashes = 1
		}
		top := style.Render("▶─[ " + lang + " ]" + strings.Repeat("─", dashes) + "┐")
		bottom := style.Render("└" + strings.Repeat("─", width-2) + "┘")
		return top, bottom
	}
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	top := muted.Render("┌─ ") +
		lipglossv2.NewStyle().Foreground(output.BrandAccent).Bold(true).Render(lang) +
		muted.Render(" "+strings.Repeat("─", remainingDashes)+"┐")
	bottom := muted.Render("└" + strings.Repeat("─", width-2) + "┘")
	return top, bottom
}

func applyInsertions(lines []string, insertions []borderInsertion) []string {
	insertAbove := map[int][]string{}
	insertBelow := map[int][]string{}
	for _, ins := range insertions {
		if ins.isAbove {
			insertAbove[ins.lineIdx] = append(insertAbove[ins.lineIdx], ins.text)
		} else {
			insertBelow[ins.lineIdx] = append(insertBelow[ins.lineIdx], ins.text)
		}
	}
	var result []string
	for i, line := range lines {
		if above, ok := insertAbove[i]; ok {
			result = append(result, above...)
		}
		result = append(result, line)
		if below, ok := insertBelow[i]; ok {
			result = append(result, below...)
		}
	}
	return result
}

func addCodeBlockBorders(rendered string, blocks []codeBlock, width, activeIdx int) (string, []codeBlock) {
	if len(blocks) == 0 {
		return rendered, blocks
	}

	strippedLines := strings.Split(ansi.Strip(rendered), "\n")
	updatedBlocks := make([]codeBlock, len(blocks))
	copy(updatedBlocks, blocks)

	blockIdx := 0
	var insertions []borderInsertion

	for lineNum := 0; lineNum < len(strippedLines) && blockIdx < len(blocks); lineNum++ {
		firstLine := firstNonEmptyCodeLine(blocks[blockIdx])
		if firstLine == "" {
			blockIdx++
			continue
		}
		if !strings.Contains(strippedLines[lineNum], strings.TrimSpace(firstLine)) {
			continue
		}

		top, bottom := renderBlockBorder(blocks[blockIdx].lang, width, blockIdx == activeIdx)
		endLine := lineNum + len(strings.Split(blocks[blockIdx].code, "\n"))
		if endLine >= len(strippedLines) {
			endLine = len(strippedLines) - 1
		}

		insertions = append(insertions,
			borderInsertion{lineIdx: lineNum, text: top, isAbove: true},
			borderInsertion{lineIdx: endLine, text: bottom, isAbove: false},
		)
		updatedBlocks[blockIdx].startLine = lineNum
		blockIdx++
	}

	if len(insertions) == 0 {
		return rendered, updatedBlocks
	}

	result := applyInsertions(strings.Split(rendered, "\n"), insertions)

	for i := range updatedBlocks {
		shift := 0
		for _, ins := range insertions {
			if ins.lineIdx < updatedBlocks[i].startLine || (ins.lineIdx == updatedBlocks[i].startLine && ins.isAbove) {
				shift++
			}
		}
		updatedBlocks[i].startLine += shift
	}

	return strings.Join(result, "\n"), updatedBlocks
}

func savingsBar(pct float64, barWidth int) string {
	if barWidth < 5 {
		barWidth = 5
	}
	if barWidth > 30 {
		barWidth = 30
	}
	filled := int(math.Round(pct / 100 * float64(barWidth)))
	if filled > barWidth {
		filled = barWidth
	}
	green := lipglossv2.NewStyle().Foreground(output.BrandSuccess)
	bar := "[" + strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled) + "]"
	return green.Render(bar) + fmt.Sprintf(" %.1f%%", pct)
}

var tuiStatusColors = map[string]lipglossv2.ANSIColor{
	"active":     output.BrandSuccess,
	"optimized":  output.BrandSuccess,
	"complete":   output.BrandSuccess,
	"failed":     output.BrandError,
	"error":      output.BrandError,
	"pending":    output.BrandWarning,
	"processing": output.BrandWarning,
	"in_review":  output.BrandPrimary,
}

func tuiStatusBadge(status string) string {
	c, ok := tuiStatusColors[strings.ToLower(status)]
	if !ok {
		c = output.BrandMuted
	}
	return lipglossv2.NewStyle().
		Bold(true).
		Foreground(lipglossv2.ANSIColor(231)).
		Background(c).
		PaddingLeft(1).
		PaddingRight(1).
		Render(status)
}

func strippedToOriginalMap(content string) (string, []int) {
	stripped := ansi.Strip(content)
	indexMap := make([]int, len(stripped))
	si := 0
	oi := 0
	for si < len(stripped) && oi < len(content) {
		if content[oi] == '\x1b' {
			for oi < len(content) && content[oi] != 'm' {
				oi++
			}
			if oi < len(content) {
				oi++
			}
			continue
		}
		indexMap[si] = oi
		si++
		oi++
	}
	return stripped, indexMap
}

func searchHighlights(content, query string) [][]int {
	if query == "" {
		return nil
	}
	stripped, indexMap := strippedToOriginalMap(content)
	re := regexp.MustCompile("(?i)" + regexp.QuoteMeta(query))
	strippedMatches := re.FindAllStringIndex(stripped, -1)
	if len(strippedMatches) == 0 {
		return nil
	}
	result := make([][]int, len(strippedMatches))
	for i, m := range strippedMatches {
		start := indexMap[m[0]]
		end := indexMap[m[1]-1] + 1
		result[i] = []int{start, end}
	}
	return result
}

func buildSummaryTab(m recommendationViewModel, width int) recommendationTab {
	green := lipglossv2.NewStyle().Foreground(output.BrandSuccess)
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)

	var lines []string
	lines = append(lines, fmt.Sprintf("  Service:      %s", m.service))
	lines = append(lines, fmt.Sprintf("  Environment:  %s", m.environment))
	lines = append(lines, fmt.Sprintf("  Status:       %s", m.status))
	if m.analysisPeriod != "" {
		lines = append(lines, fmt.Sprintf("  Analysis:     %s", formatDate(m.analysisPeriod)))
	}
	if m.createdAt != "" {
		lines = append(lines, fmt.Sprintf("  Created:      %s", relativeTime(m.createdAt)))
	}
	if m.description != "" {
		lines = append(lines, "")
		wrapped := wordWrap(m.description, width-4)
		for _, wl := range strings.Split(wrapped, "\n") {
			lines = append(lines, "  "+wl)
		}
	}

	lines = append(lines, "")
	lines = append(lines, "  "+muted.Render(strings.Repeat("─", width-4)))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  Current Spending:  $%.2f/mo", m.currentSpending))
	lines = append(lines, fmt.Sprintf("  Monthly Savings:   %s", green.Render(fmt.Sprintf("$%.2f/mo", m.monthlySavings))))
	lines = append(lines, fmt.Sprintf("  Annual Savings:    %s", green.Render(fmt.Sprintf("$%.2f/yr", m.annualSavings))))
	if m.pct > 0 {
		lines = append(lines, fmt.Sprintf("  Savings:           %s", green.Render(fmt.Sprintf("%.1f%%", m.pct))))
		lines = append(lines, "")
		lines = append(lines, "  "+savingsBar(m.pct, 20))
	}
	return recommendationTab{title: "Summary", content: strings.Join(lines, "\n")}
}

func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n")
}

func markdownTab(title, raw string, width int) recommendationTab {
	placeholder := lipglossv2.NewStyle().Foreground(output.BrandMuted).Render("  No content available.")
	if raw == "" {
		return recommendationTab{title: title, raw: raw, content: placeholder}
	}
	blocks := parseCodeBlocks(raw)
	rendered := renderMarkdownContent(raw, width)
	content, blocks := addCodeBlockBorders(rendered, blocks, width, -1)
	return recommendationTab{title: title, raw: raw, rendered: rendered, content: content, codeBlocks: blocks}
}

func mergeImplementationRaw(impl, exec string) string {
	if impl == "" {
		return exec
	}
	if exec == "" {
		return impl
	}
	return impl + "\n\n---\n\n" + exec
}

func buildTabs(m recommendationViewModel, width int) []recommendationTab {
	return []recommendationTab{
		buildSummaryTab(m, width),
		markdownTab("Analysis", m.rawKeyTakeaway, width),
		markdownTab("Implementation", mergeImplementationRaw(m.rawImplementation, m.rawExecution), width),
	}
}

func rerenderTabs(m recommendationViewModel, width int) []recommendationTab {
	return []recommendationTab{
		buildSummaryTab(m, width),
		markdownTab(m.tabs[1].title, m.tabs[1].raw, width),
		markdownTab(m.tabs[2].title, m.tabs[2].raw, width),
	}
}

func newBaseModel() recommendationViewModel {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.CharLimit = 100

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipglossv2.NewStyle().Foreground(output.BrandPrimary)

	h := help.New()
	h.Styles.ShortKey = lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	h.Styles.ShortDesc = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.ShortSeparator = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.FullKey = lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	h.Styles.FullDesc = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.FullSeparator = lipglossv2.NewStyle().Foreground(output.BrandMuted)

	return recommendationViewModel{
		activeBlock: -1,
		searchInput: ti,
		help:        h,
		spinner:     sp,
	}
}

func (m *recommendationViewModel) populateData(data map[string]interface{}) {
	m.recID = fmt.Sprintf("%v", data["recommendation_id"])
	m.service = fmt.Sprintf("%v", data["service"])
	m.environment = fmt.Sprintf("%v", data["environment"])
	m.status = fmt.Sprintf("%v", data["status"])
	m.createdAt, _ = data["created_at"].(string)
	m.analysisPeriod, _ = data["analysis_period"].(string)
	m.currentSpending = toFloat(data["current_spending"])
	m.monthlySavings = toFloat(data["monthly_savings"])
	m.annualSavings = toFloat(data["annual_savings"])
	m.pct = toFloat(data["savings_percentage"])

	actions, _ := data["actions"].(map[string]interface{})
	if actions != nil {
		m.description, _ = actions["description"].(string)
		m.rawKeyTakeaway, _ = actions["key_takeaway"].(string)
		m.rawImplementation, _ = actions["implementation_process"].(string)
		m.rawExecution, _ = actions["execution_method"].(string)
	}
}

func newRecommendationViewModel(data map[string]interface{}) recommendationViewModel {
	m := newBaseModel()
	m.populateData(data)
	return m
}

func newReadyViewModel(data map[string]interface{}, width, height int) recommendationViewModel {
	m := newBaseModel()
	m.populateData(data)
	m.width = width
	m.height = height

	headerHeight := 4
	footerHeight := 1
	vpHeight := height - headerHeight - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	usableWidth := width - 4
	if usableWidth < 20 {
		usableWidth = 20
	}

	m.tabs = buildTabs(m, usableWidth)
	m.viewport = viewport.New(viewport.WithWidth(width), viewport.WithHeight(vpHeight))
	m.viewport.SoftWrap = true
	m.refreshBlockHighlight()
	m.help.SetWidth(width)
	m.ready = true
	return m
}

func (m recommendationViewModel) ShortHelp() []key.Binding {
	switch m.mode {
	case tuicommon.ModeSearch:
		return []key.Binding{keys.Enter, keys.NextMatch, keys.PrevMatch, keys.Cancel}
	case tuicommon.ModeHelp:
		return []key.Binding{keys.CloseHelp, keys.Quit}
	default:
		bindings := []key.Binding{keys.Tabs, keys.Left, keys.Right, keys.Search, keys.Copy}
		if m.hasCodeBlocks() {
			bindings = append(bindings, keys.NextBlock, keys.PrevBlock)
		}
		bindings = append(bindings, keys.Open, keys.Help, keys.Quit)
		return bindings
	}
}

func (m recommendationViewModel) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{keys.Tabs, keys.Left, keys.Right, keys.Up, keys.Down},
		{keys.Search, keys.NextMatch, keys.PrevMatch, keys.Copy, keys.CopyAll, keys.Open},
		{keys.NextBlock, keys.PrevBlock, keys.Help, keys.Quit},
	}
}

func (m recommendationViewModel) hasCodeBlocks() bool {
	if len(m.tabs) == 0 {
		return false
	}
	return len(m.tabs[m.activeTab].codeBlocks) > 0
}

func (m recommendationViewModel) Init() tea.Cmd {
	if m.ready {
		return nil
	}
	return m.spinner.Tick
}

func (m *recommendationViewModel) applySearchHighlights() {
	if m.searchQuery == "" {
		m.viewport.ClearHighlights()
		m.matchCount = 0
		return
	}
	matches := searchHighlights(m.tabs[m.activeTab].content, m.searchQuery)
	m.matchCount = len(matches)
	if len(matches) == 0 {
		m.viewport.ClearHighlights()
		return
	}
	m.viewport.HighlightStyle = lipglossv2.NewStyle().Background(lipglossv2.ANSIColor(226))
	m.viewport.SelectedHighlightStyle = lipglossv2.NewStyle().Background(lipglossv2.ANSIColor(214)).Bold(true)
	m.viewport.SetHighlights(matches)
}

func (m *recommendationViewModel) refreshBlockHighlight() {
	tab := &m.tabs[m.activeTab]
	if tab.rendered == "" || len(tab.codeBlocks) == 0 {
		m.viewport.SetContent(tab.content)
		return
	}
	width := m.width - 4
	if width < 20 {
		width = 20
	}
	content, blocks := addCodeBlockBorders(tab.rendered, tab.codeBlocks, width, m.activeBlock)
	tab.content = content
	tab.codeBlocks = blocks
	m.viewport.SetContent(content)
}

func (m *recommendationViewModel) switchTab(idx int) {
	if idx < 0 || idx >= len(m.tabs) {
		return
	}
	m.activeTab = idx
	m.activeBlock = -1
	m.refreshBlockHighlight()
	m.viewport.GotoTop()
	m.applySearchHighlights()
}

func (m *recommendationViewModel) updateHelp(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, keys.CloseHelp) || key.Matches(msg, keys.Quit) {
		if msg.String() == "q" {
			return tea.Quit
		}
		m.mode = tuicommon.ModeNormal
	}
	return nil
}

func (m *recommendationViewModel) updateSearch(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = tuicommon.ModeNormal
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.viewport.ClearHighlights()
		m.matchCount = 0
		return nil
	case key.Matches(msg, keys.Enter):
		m.mode = tuicommon.ModeNormal
		m.searchQuery = m.searchInput.Value()
		m.searchInput.Blur()
		m.applySearchHighlights()
		if m.matchCount > 0 {
			m.viewport.HighlightNext()
		}
		return nil
	default:
		var cmd tea.Cmd
		m.searchInput, cmd = m.searchInput.Update(msg)
		return cmd
	}
}

func (m *recommendationViewModel) navigateBlock(dir int) tea.Cmd {
	blocks := m.tabs[m.activeTab].codeBlocks
	if len(blocks) == 0 {
		return nil
	}
	prev := m.activeBlock
	switch {
	case dir > 0:
		m.activeBlock = (m.activeBlock + 1) % len(blocks)
	case m.activeBlock <= 0:
		m.activeBlock = len(blocks) - 1
	default:
		m.activeBlock--
	}

	m.refreshBlockHighlight()
	blocks = m.tabs[m.activeTab].codeBlocks

	offset := blocks[m.activeBlock].startLine - 5
	if offset < 0 {
		offset = 0
	}
	m.viewport.SetYOffset(offset)
	m.applySearchHighlights()

	if dir > 0 && prev >= 0 && m.activeBlock < prev {
		m.feedback = "↻ Wrapped to first block"
		return clearFeedback()
	}
	if dir < 0 && prev >= 0 && m.activeBlock > prev {
		m.feedback = "↻ Wrapped to last block"
		return clearFeedback()
	}
	return nil
}

func (m *recommendationViewModel) copyContent(all bool) tea.Cmd {
	if !all {
		blocks := m.tabs[m.activeTab].codeBlocks
		if len(blocks) > 0 {
			if m.activeBlock < 0 {
				m.activeBlock = 0
				m.refreshBlockHighlight()
				m.applySearchHighlights()
			}
			lang := blocks[m.activeBlock].lang
			if lang == "" {
				lang = "code"
			}
			m.feedback = fmt.Sprintf("Copied %s block (%d/%d)", lang, m.activeBlock+1, len(blocks))
			return tea.Batch(tea.SetClipboard(blocks[m.activeBlock].code), clearFeedback())
		}
	}
	raw := m.tabs[m.activeTab].raw
	if raw == "" {
		raw = ansi.Strip(m.tabs[m.activeTab].content)
	}
	m.feedback = feedbackCopied
	return tea.Batch(tea.SetClipboard(raw), clearFeedback())
}

func (m *recommendationViewModel) handleTabNav(msg tea.KeyPressMsg) bool {
	switch {
	case key.Matches(msg, keys.Tabs):
		m.switchTab(int(msg.String()[0] - '1'))
	case key.Matches(msg, keys.Right):
		m.switchTab((m.activeTab + 1) % len(m.tabs))
	case msg.String() == "shift+tab", key.Matches(msg, keys.Left):
		m.switchTab((m.activeTab - 1 + len(m.tabs)) % len(m.tabs))
	default:
		return false
	}
	return true
}

func (m *recommendationViewModel) updateNormal(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return true, tea.Quit
	case key.Matches(msg, keys.Help):
		m.mode = tuicommon.ModeHelp
		return true, nil
	case key.Matches(msg, keys.Search):
		m.mode = tuicommon.ModeSearch
		m.searchInput.SetValue("")
		return true, m.searchInput.Focus()
	case m.handleTabNav(msg):
		return true, nil
	case key.Matches(msg, keys.NextMatch):
		if m.searchQuery != "" {
			m.viewport.HighlightNext()
		}
		return true, nil
	case key.Matches(msg, keys.PrevMatch):
		if m.searchQuery != "" {
			m.viewport.HighlightPrevious()
		}
		return true, nil
	case key.Matches(msg, keys.NextBlock):
		return true, m.navigateBlock(1)
	case key.Matches(msg, keys.PrevBlock):
		return true, m.navigateBlock(-1)
	case key.Matches(msg, keys.Copy):
		return true, m.copyContent(false)
	case key.Matches(msg, keys.CopyAll):
		return true, m.copyContent(true)
	case key.Matches(msg, keys.Open):
		url := fmt.Sprintf("https://dashboard.levelfour.ai/savings-recommendations?recommendation=%s", m.recID)
		_ = openBrowser(url)
		m.feedback = "Opening in browser..."
		return true, clearFeedback()
	default:
		return false, nil
	}
}

func (m recommendationViewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		usableWidth := m.width - 4
		if usableWidth < 20 {
			usableWidth = 20
		}
		vpHeight := m.height - 4 - 1
		if vpHeight < 1 {
			vpHeight = 1
		}
		if len(m.tabs) > 0 {
			m.tabs = rerenderTabs(m, usableWidth)
		} else {
			m.tabs = buildTabs(m, usableWidth)
		}
		m.viewport = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(vpHeight))
		m.viewport.SoftWrap = true
		m.refreshBlockHighlight()
		m.help.SetWidth(m.width)
		m.applySearchHighlights()
		m.ready = true
		return m, nil

	case spinner.TickMsg:
		if !m.ready {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case clearFeedbackMsg:
		m.feedback = ""
		return m, nil

	case tea.KeyPressMsg:
		mp := &m
		switch m.mode {
		case tuicommon.ModeHelp:
			return m, mp.updateHelp(msg)
		case tuicommon.ModeSearch:
			return m, mp.updateSearch(msg)
		default:
			handled, cmd := mp.updateNormal(msg)
			if handled {
				return m, cmd
			}
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m recommendationViewModel) View() tea.View {
	if !m.ready {
		v := tea.NewView(m.spinner.View() + " Loading recommendation...")
		v.AltScreen = true
		return v
	}

	primary := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)

	titleLine := buildTitleLine(m, primary, muted)
	subtitle := muted.Render(fmt.Sprintf("%s / %s", m.service, m.environment))
	separator := muted.Render(strings.Repeat("─", m.width))
	tabBar := buildTabBar(m, primary, muted)

	header := titleLine + "\n" + subtitle + "\n" + separator + "\n" + tabBar

	var body string
	if m.mode == tuicommon.ModeHelp {
		body = renderHelpOverlay(m)
	} else {
		body = m.viewport.View()
	}

	footer := buildFooter(m, muted)

	s := header + "\n" + body + "\n" + footer

	v := tea.NewView(s)
	v.AltScreen = true
	return v
}

func rightAlign(left, right string, width int) string {
	padding := width - lipglossv2.Width(left) - lipglossv2.Width(right)
	if padding < 1 {
		return left + " " + right
	}
	return left + strings.Repeat(" ", padding) + right
}

func buildTitleLine(m recommendationViewModel, primary, _ lipglossv2.Style) string {
	title := primary.Render(fmt.Sprintf("Recommendation %s", m.recID))
	badge := tuiStatusBadge(m.status)
	return rightAlign(title, badge, m.width)
}

func buildTabBar(m recommendationViewModel, _, muted lipglossv2.Style) string {
	var parts []string
	for i, tab := range m.tabs {
		if i == m.activeTab {
			style := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true).Underline(true)
			parts = append(parts, style.Render(tab.title))
		} else {
			parts = append(parts, muted.Render(tab.title))
		}
	}
	tabStr := strings.Join(parts, muted.Render(" │ "))
	indicator := muted.Render(fmt.Sprintf("[%d/%d]", m.activeTab+1, len(m.tabs)))
	return rightAlign(tabStr, indicator, m.width)
}

func buildFooter(m recommendationViewModel, muted lipglossv2.Style) string {
	scrollPct := muted.Render(fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100))

	if m.mode == tuicommon.ModeSearch {
		inputView := m.searchInput.View()
		matchInfo := muted.Render(fmt.Sprintf("[%d matches]", m.matchCount))
		searchHelp := m.help.View(&m)
		left := inputView + "  " + matchInfo + "  " + searchHelp
		return rightAlign(left, scrollPct, m.width)
	}

	if m.feedback != "" {
		fb := lipglossv2.NewStyle().Foreground(output.BrandSuccess).Bold(true).Render(m.feedback)
		return rightAlign(fb, scrollPct, m.width)
	}

	helpView := m.help.View(&m)
	blocks := m.tabs[m.activeTab].codeBlocks
	if len(blocks) > 0 && m.activeBlock >= 0 {
		blockInfo := lipglossv2.NewStyle().Foreground(output.BrandAccent).Bold(true).
			Render(fmt.Sprintf("Block %d/%d", m.activeBlock+1, len(blocks)))
		left := blockInfo + "  " + helpView
		return rightAlign(left, scrollPct, m.width)
	}
	return rightAlign(helpView, scrollPct, m.width)
}

func renderHelpOverlay(m recommendationViewModel) string {
	headerHeight := 4
	footerHeight := 1
	availHeight := m.height - headerHeight - footerHeight
	if availHeight < 1 {
		availHeight = 1
	}

	title := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true).Render("Keyboard Shortcuts")
	m.help.ShowAll = true
	content := m.help.View(&m)
	m.help.ShowAll = false

	helpBlock := title + "\n\n" + content
	helpLines := strings.Count(helpBlock, "\n") + 1

	if helpLines < availHeight {
		helpBlock += strings.Repeat("\n", availHeight-helpLines)
	}

	return lipglossv2.Place(m.width, availHeight, lipglossv2.Center, lipglossv2.Center, helpBlock)
}

func startTUIWithModel(m recommendationViewModel, opts ...tea.ProgramOption) error {
	_, err := tea.NewProgram(m, opts...).Run()
	return err
}

func startTUI(data map[string]interface{}, opts ...tea.ProgramOption) error {
	m := newRecommendationViewModel(data)
	return startTUIWithModel(m, opts...)
}

var runRecommendationViewTUI = func(data map[string]interface{}) error {
	tty, err := os.Open("/dev/tty")
	if err != nil {
		m := newRecommendationViewModel(data)
		return startTUIWithModel(m)
	}
	defer func() { _ = tty.Close() }()
	w, h := 80, 24
	if ww, hh, sizeErr := term.GetSize(int(tty.Fd())); sizeErr == nil {
		w, h = ww, hh
	}
	m := newReadyViewModel(data, w, h)
	opts := []tea.ProgramOption{
		tea.WithInput(tty),
		tea.WithColorProfile(colorprofile.ANSI256),
		tea.WithFilter(tuicommon.FilterTerminalNoise),
		tea.WithWindowSize(w, h),
	}
	return startTUIWithModel(m, opts...)
}
