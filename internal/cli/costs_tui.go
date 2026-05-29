package cli

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	btable "charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

type costsKeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Enter        key.Binding
	CloseDetail  key.Binding
	NextPage     key.Binding
	PrevPage     key.Binding
	Sort         key.Binding
	SortOrder    key.Binding
	Search       key.Binding
	Filter       key.Binding
	ClearFilters key.Binding
	CycleDim     key.Binding
	Open         key.Binding
	Help         key.Binding
	Quit         key.Binding
	Cancel       key.Binding
	Confirm      key.Binding
}

var costsKeys = costsKeyMap{
	Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:         key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:        key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "toggle detail")),
	CloseDetail:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "close detail")),
	NextPage:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")),
	PrevPage:     key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prev page")),
	Sort:         key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort column")),
	SortOrder:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sort order")),
	Search:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Filter:       key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "filter")),
	ClearFilters: key.NewBinding(key.WithKeys("F"), key.WithHelp("F", "clear filters")),
	CycleDim:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "cycle dimension")),
	Open:         key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	Help:         key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:         key.NewBinding(key.WithKeys("q"), key.WithHelp("q", "quit")),
	Cancel:       key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Confirm:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
}

func (k costsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Search, k.Filter, k.Sort, k.NextPage, k.PrevPage, k.Help, k.Quit}
}

func (k costsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.CloseDetail},
		{k.Search, k.Filter, k.ClearFilters, k.CycleDim},
		{k.Sort, k.SortOrder},
		{k.NextPage, k.PrevPage, k.Open},
		{k.Help, k.Quit},
	}
}

type filterDimSpec struct {
	label string
	get   func(*costsFilterState) *[]string
}

var costsFilterDims = []filterDimSpec{
	{"service", func(s *costsFilterState) *[]string { return &s.service }},
	{"region", func(s *costsFilterState) *[]string { return &s.region }},
	{"account", func(s *costsFilterState) *[]string { return &s.account }},
	{"environment", func(s *costsFilterState) *[]string { return &s.environment }},
	{"tag-key", func(s *costsFilterState) *[]string { return &s.tagKey }},
	{"tag-value", func(s *costsFilterState) *[]string { return &s.tagValue }},
}

var costsSortableColumns = []struct {
	label string
	key   string
}{
	{"cost", "cost"},
	{"previous", "previous_cost"},
	{"change %", "change_percentage"},
	{"service", "service"},
	{"region", "region"},
	{"account", "account_id"},
}

type costsListFetchMsg struct {
	data       *levelfourgo.ProviderServiceBreakdownData
	items      []*levelfourgo.ProviderServiceBreakdownItem
	pagination *levelfourgo.PaginationMeta
	err        error
}

type costsModel struct {
	items      []*levelfourgo.ProviderServiceBreakdownItem
	allItems   []*levelfourgo.ProviderServiceBreakdownItem
	breakdown  *levelfourgo.ProviderServiceBreakdownData
	providerID string
	baseState  costsFilterState

	table      btable.Model
	detail     viewport.Model
	detailOpen bool
	detailRow  *levelfourgo.ProviderServiceBreakdownItem

	pagination *levelfourgo.PaginationMeta
	page       int
	pageSize   int

	mode        tuicommon.Mode
	searchInput textinput.Model
	searchQuery string

	filterInput textinput.Model
	filterDim   int

	sortIdx   int
	sortOrder string

	width   int
	height  int
	ready   bool
	loading bool

	help     help.Model
	spinner  spinner.Model
	feedback string

	client *api.SDKClient
}

func newCostsModel(
	client *api.SDKClient,
	providerID string,
	data *levelfourgo.ProviderServiceBreakdownData,
	items []*levelfourgo.ProviderServiceBreakdownItem,
	pagination *levelfourgo.PaginationMeta,
	pageSize int,
	base costsFilterState,
) costsModel {
	si := textinput.New()
	si.Prompt = "/ "
	si.Placeholder = "search..."

	fi := textinput.New()
	fi.Prompt = "value: "
	fi.Placeholder = "type value, enter to apply..."

	h := help.New()
	h.Styles.ShortKey = lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	h.Styles.ShortDesc = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.ShortSeparator = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.FullKey = lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	h.Styles.FullDesc = lipglossv2.NewStyle().Foreground(output.BrandMuted)
	h.Styles.FullSeparator = lipglossv2.NewStyle().Foreground(output.BrandMuted)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	sp.Style = lipglossv2.NewStyle().Foreground(output.BrandPrimary)

	pg := 1
	if pagination != nil {
		pg = pagination.GetCurrentPage()
	}

	sortOrder := base.sortOrder
	if sortOrder == "" {
		sortOrder = sortOrderDesc
	}

	return costsModel{
		items:       items,
		allItems:    items,
		breakdown:   data,
		providerID:  providerID,
		baseState:   base,
		pagination:  pagination,
		page:        pg,
		pageSize:    pageSize,
		searchInput: si,
		filterInput: fi,
		sortIdx:     0,
		sortOrder:   sortOrder,
		help:        h,
		spinner:     sp,
		client:      client,
	}
}

func (m costsModel) Init() tea.Cmd {
	if m.ready {
		return nil
	}
	return m.spinner.Tick
}

func (m costsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table = m.buildTable()
		m.detail = m.buildDetail()
		m.applySearch()
		m.ready = true
		return m, nil

	case spinner.TickMsg:
		if m.loading || !m.ready {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case costsListFetchMsg:
		m.loading = false
		if msg.err != nil {
			m.feedback = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.breakdown = msg.data
		m.allItems = msg.items
		m.items = msg.items
		m.pagination = msg.pagination
		if msg.pagination != nil {
			m.page = msg.pagination.GetCurrentPage()
		}
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.table = m.buildTable()
		if m.detailOpen {
			m.detailOpen = false
			m.detailRow = nil
		}
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case tuicommon.ModeHelp:
			return m.updateCostsHelp(msg)
		case tuicommon.ModeSearch:
			return m.updateCostsSearch(msg)
		case tuicommon.ModeFilter:
			return m.updateCostsFilter(msg)
		default:
			return m.updateCostsNormal(msg)
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m costsModel) updateCostsNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, costsKeys.Quit):
		return m, tea.Quit

	case key.Matches(msg, costsKeys.CloseDetail):
		if m.detailOpen {
			m.detailOpen = false
			m.detailRow = nil
			m.table = m.buildTable()
			return m, nil
		}
		return m, tea.Quit

	case key.Matches(msg, costsKeys.Enter):
		return m.toggleDetailForSelected()

	case key.Matches(msg, costsKeys.Search):
		m.mode = tuicommon.ModeSearch
		m.searchInput.Focus()
		return m, nil

	case key.Matches(msg, costsKeys.Filter):
		if m.detailOpen {
			m.detailOpen = false
			m.detailRow = nil
			m.table = m.buildTable()
		}
		m.mode = tuicommon.ModeFilter
		m.filterInput.SetValue("")
		m.filterInput.Focus()
		return m, nil

	case key.Matches(msg, costsKeys.ClearFilters):
		return m.clearAllFilters()

	case key.Matches(msg, costsKeys.Help):
		m.mode = tuicommon.ModeHelp
		return m, nil

	case key.Matches(msg, costsKeys.Sort), key.Matches(msg, costsKeys.SortOrder):
		return m.handleCostsSort(msg)

	case key.Matches(msg, costsKeys.NextPage), key.Matches(msg, costsKeys.PrevPage):
		return m.handleCostsPageNav(msg)

	case key.Matches(msg, costsKeys.Open):
		_ = openWeb("/spending")
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	if m.detailOpen {
		m.refreshDetailFromSelected()
	}
	return m, cmd
}

func (m costsModel) toggleDetailForSelected() (tea.Model, tea.Cmd) {
	if m.detailOpen {
		m.detailOpen = false
		m.detailRow = nil
		m.table = m.buildTable()
		return m, nil
	}
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.items) {
		return m, nil
	}
	m.detailRow = m.items[cursor]
	m.detailOpen = true
	m.table = m.buildTable()
	m.detail = m.buildDetail()
	m.detail.SetContent(m.renderDetailContent(m.detailRow))
	return m, nil
}

func (m *costsModel) refreshDetailFromSelected() {
	if !m.detailOpen {
		return
	}
	cursor := m.table.Cursor()
	if cursor < 0 || cursor >= len(m.items) {
		return
	}
	m.detailRow = m.items[cursor]
	m.detail.SetContent(m.renderDetailContent(m.detailRow))
}

func (m costsModel) handleCostsSort(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, costsKeys.Sort) {
		m.sortIdx = (m.sortIdx + 1) % len(costsSortableColumns)
	} else {
		if m.sortOrder == sortOrderDesc {
			m.sortOrder = sortOrderAsc
		} else {
			m.sortOrder = sortOrderDesc
		}
	}
	m.feedback = fmt.Sprintf("Sort: %s %s", costsSortableColumns[m.sortIdx].label, m.costsSortArrow())
	m.loading = true
	return m, tea.Batch(m.fetchCostsPage(), m.spinner.Tick)
}

func (m costsModel) handleCostsPageNav(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := tuicommon.ResolvePageNav(msg, m.page, m.pagination, costsKeys.NextPage, costsKeys.PrevPage)
	if !r.Changed {
		return m, nil
	}
	m.page = r.Page
	m.loading = true
	return m, tea.Batch(m.fetchCostsPage(), m.spinner.Tick)
}

func (m costsModel) updateCostsSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	action := tuicommon.ResolveSearchAction(msg, costsKeys.Cancel, costsKeys.Confirm)

	if action == tuicommon.SearchActionCancel {
		m.mode = tuicommon.ModeNormal
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.searchInput.Blur()
		m.items = m.allItems
		m.table = m.buildTable()
		return m, nil
	}
	if action == tuicommon.SearchActionConfirm {
		m.mode = tuicommon.ModeNormal
		m.searchQuery = m.searchInput.Value()
		m.searchInput.Blur()
		m.applySearch()
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.searchQuery = m.searchInput.Value()
	m.applySearch()
	return m, cmd
}

func (m costsModel) updateCostsHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, costsKeys.Help), key.Matches(msg, costsKeys.Cancel):
		m.mode = tuicommon.ModeNormal
		return m, nil
	case key.Matches(msg, costsKeys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

func (m costsModel) updateCostsFilter(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, costsKeys.Cancel):
		m.mode = tuicommon.ModeNormal
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		return m, nil

	case key.Matches(msg, costsKeys.CycleDim):
		m.filterDim = (m.filterDim + 1) % len(costsFilterDims)
		m.filterInput.SetValue("")
		return m, nil

	case key.Matches(msg, costsKeys.Confirm):
		value := strings.TrimSpace(m.filterInput.Value())
		if value == "" {
			m.mode = tuicommon.ModeNormal
			m.filterInput.Blur()
			return m, nil
		}
		dim := costsFilterDims[m.filterDim]
		vals := dim.get(&m.baseState)
		if !containsString(*vals, value) {
			*vals = append(*vals, value)
		}
		m.mode = tuicommon.ModeNormal
		m.filterInput.Blur()
		m.filterInput.SetValue("")
		m.feedback = fmt.Sprintf("Filter applied: %s=%s", dim.label, value)
		m.loading = true
		return m, tea.Batch(m.fetchCostsPage(), m.spinner.Tick)
	}

	var cmd tea.Cmd
	m.filterInput, cmd = m.filterInput.Update(msg)
	return m, cmd
}

func (m costsModel) clearAllFilters() (tea.Model, tea.Cmd) {
	active := activeFilterSummary(m.baseState)
	if active == "" {
		return m, nil
	}
	for _, dim := range costsFilterDims {
		vals := dim.get(&m.baseState)
		*vals = nil
	}
	m.feedback = "Filters cleared"
	m.loading = true
	return m, tea.Batch(m.fetchCostsPage(), m.spinner.Tick)
}

func activeFilterSummary(s costsFilterState) string {
	var parts []string
	for _, dim := range costsFilterDims {
		vals := dim.get(&s)
		if len(*vals) > 0 {
			parts = append(parts, fmt.Sprintf("%s=%s", dim.label, strings.Join(*vals, ",")))
		}
	}
	return strings.Join(parts, " · ")
}

func (m *costsModel) applySearch() {
	if m.searchQuery == "" {
		m.items = m.allItems
	} else {
		query := strings.ToLower(m.searchQuery)
		var filtered []*levelfourgo.ProviderServiceBreakdownItem
		for _, item := range m.allItems {
			if costItemMatchesSearch(item, query) {
				filtered = append(filtered, item)
			}
		}
		m.items = filtered
	}
	m.table.SetRows(m.buildTableRows())
}

func costItemMatchesSearch(item *levelfourgo.ProviderServiceBreakdownItem, query string) bool {
	fields := []string{
		fmt.Sprintf("$%.2f", item.GetCost()),
	}
	if s := item.GetService(); s != nil {
		fields = append(fields, *s)
	}
	if r := item.GetRegion(); r != nil {
		fields = append(fields, *r)
	}
	if a := item.GetAccountID(); a != nil {
		fields = append(fields, *a)
	}
	if e := item.GetEnvironment(); e != nil {
		fields = append(fields, *e)
	}
	if k := item.GetTagKey(); k != nil {
		fields = append(fields, *k)
	}
	if v := item.GetTagValue(); v != nil {
		fields = append(fields, *v)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

func (m costsModel) buildTable() btable.Model {
	cols := m.buildTableColumns()
	rows := m.buildTableRows()

	s := btable.Styles{
		Header: lipglossv2.NewStyle().Bold(true).Foreground(output.BrandPrimary),
		Cell:   lipglossv2.NewStyle(),
		Selected: lipglossv2.NewStyle().
			Bold(true).
			Foreground(lipglossv2.ANSIColor(231)).
			Background(output.BrandPrimary),
	}

	tableHeight := m.listAreaHeight()

	t := btable.New(
		btable.WithColumns(cols),
		btable.WithRows(rows),
		btable.WithHeight(tableHeight),
		btable.WithWidth(m.width),
		btable.WithFocused(true),
		btable.WithStyles(s),
	)
	return t
}

func (m costsModel) buildTableColumns() []btable.Column {
	w := m.width
	active := activeCostColumns(w, m.baseState.groupBy)

	type colDef struct {
		title  string
		weight int
	}
	defs := make([]colDef, 0, len(active))
	for _, c := range active {
		weight := 2
		switch c.header {
		case columnService, columnAccount, "Tag Value":
			weight = 3
		case "Cost", "Prev Cost":
			weight = 2
		case "Change":
			weight = 1
		}
		defs = append(defs, colDef{title: c.header, weight: weight})
	}

	totalWeight := 0
	for _, d := range defs {
		totalWeight += d.weight
	}
	if totalWeight == 0 {
		totalWeight = 1
	}

	usable := w - len(defs) - 1
	if usable < len(defs)*5 {
		usable = len(defs) * 5
	}

	cols := make([]btable.Column, len(defs))
	for i, d := range defs {
		cw := (usable * d.weight) / totalWeight
		if cw < 5 {
			cw = 5
		}
		cols[i] = btable.Column{Title: d.title, Width: cw}
	}
	return cols
}

func (m costsModel) buildTableRows() []btable.Row {
	_, rows := buildCostsBreakdownRows(m.items, m.width, m.baseState.groupBy)
	converted := make([]btable.Row, len(rows))
	for i, r := range rows {
		converted[i] = btable.Row(r)
	}
	return converted
}

func (m costsModel) buildDetail() viewport.Model {
	height := m.detailAreaHeight()
	if height < 1 {
		height = 1
	}
	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(height))
	vp.SoftWrap = true
	return vp
}

func (m costsModel) listAreaHeight() int {
	h := m.height - m.kpiHeaderHeight() - 2
	if m.detailOpen {
		h -= m.detailAreaHeight() + 1
	}
	if h < 3 {
		h = 3
	}
	return h
}

func (m costsModel) detailAreaHeight() int {
	available := m.height - m.kpiHeaderHeight() - 2
	if available < 10 {
		return 0
	}
	h := available * 45 / 100
	if h < 6 {
		h = 6
	}
	if h > 16 {
		h = 16
	}
	return h
}

func (m costsModel) kpiHeaderHeight() int {
	return 4
}

func (m costsModel) View() tea.View {
	if !m.ready {
		lv := tea.NewView(fmt.Sprintf("\n  %s Loading cost breakdown...\n", m.spinner.View()))
		lv.AltScreen = true
		return lv
	}

	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	primary := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)

	var b strings.Builder

	b.WriteString(m.renderCostsKPIHeader())
	b.WriteString("\n")

	switch {
	case m.loading:
		fmt.Fprintf(&b, "  %s Loading...\n", m.spinner.View())
	case m.mode == tuicommon.ModeHelp:
		b.WriteString(m.renderCostsHelp())
	default:
		b.WriteString(m.table.View())
		b.WriteString("\n")
		if m.mode == tuicommon.ModeFilter {
			b.WriteString(m.renderFilterDrawer())
			b.WriteString("\n")
		} else if m.detailOpen {
			b.WriteString(m.renderDetailPane())
			b.WriteString("\n")
		}
	}

	var footer strings.Builder
	switch m.mode {
	case tuicommon.ModeSearch:
		footer.WriteString("  " + m.searchInput.View())
		if m.searchQuery != "" {
			footer.WriteString(muted.Render(fmt.Sprintf("  [%d matches]", len(m.items))))
		}
	case tuicommon.ModeFilter:
		footer.WriteString(muted.Render("  tab=cycle dimension · enter=apply · esc=cancel"))
	default:
		if m.pagination != nil {
			footer.WriteString(muted.Render(fmt.Sprintf("  Page %d/%d (%d items)",
				m.page, m.pagination.GetTotalPages(), m.pagination.GetTotalItems())))
		}
		footer.WriteString(muted.Render("  │  "))
		footer.WriteString(primary.Render(fmt.Sprintf("Sort: %s %s", costsSortableColumns[m.sortIdx].label, m.costsSortArrow())))
		if active := activeFilterSummary(m.baseState); active != "" {
			footer.WriteString(muted.Render("  │  "))
			footer.WriteString(lipglossv2.NewStyle().Foreground(output.BrandAccent).Render("Filters: " + active))
		}
		if m.feedback != "" {
			footer.WriteString(muted.Render("  │  "))
			footer.WriteString(lipglossv2.NewStyle().Foreground(output.BrandSuccess).Render(m.feedback))
		}
		footer.WriteString(muted.Render("  │  "))
		footer.WriteString(muted.Render(m.help.View(&costsKeys)))
	}
	b.WriteString(footer.String())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m costsModel) renderCostsKPIHeader() string {
	if m.breakdown == nil {
		return ""
	}
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	bold := lipglossv2.NewStyle().Bold(true)

	cardWidth := (m.width - 12) / 4
	if cardWidth < 18 {
		cardWidth = 18
	}

	rows := 0
	if m.pagination != nil {
		rows = m.pagination.GetTotalItems()
	} else {
		rows = len(m.items)
	}

	cards := []struct{ label, value string }{
		{"Period Total", fmt.Sprintf("$%.2f", m.breakdown.GetTotalPeriodCost())},
		{"Range", formatRangeLabel(m.breakdown.GetStartDate(), m.breakdown.GetEndDate())},
		{"Rows", fmt.Sprintf("%d", rows)},
		{"Provider", m.breakdown.GetProviderName()},
	}

	var rendered []string
	for _, c := range cards {
		if c.value == "" {
			c.value = dashSymbol
		}
		content := muted.Render(c.label) + "\n" + bold.Render(c.value)
		card := lipglossv2.NewStyle().
			Border(lipglossv2.RoundedBorder()).
			BorderForeground(output.BrandMuted).
			PaddingLeft(1).
			PaddingRight(1).
			Width(cardWidth).
			Render(content)
		rendered = append(rendered, card)
	}

	return lipglossv2.JoinHorizontal(lipglossv2.Top, rendered...)
}

func (m costsModel) renderDetailPane() string {
	if m.detailRow == nil {
		return ""
	}
	border := lipglossv2.NewStyle().
		Border(lipglossv2.RoundedBorder()).
		BorderForeground(output.BrandAccent).
		Width(m.width - 2)
	return border.Render(m.detail.View())
}

func (m costsModel) renderDetailContent(item *levelfourgo.ProviderServiceBreakdownItem) string {
	if item == nil {
		return ""
	}
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	primary := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	accent := lipglossv2.NewStyle().Foreground(output.BrandAccent).Bold(true)

	var b strings.Builder
	b.WriteString(accent.Render(formatDetailHeader(item)))
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "%s %s\n", muted.Render("Cost:        "), primary.Render(fmt.Sprintf("$%.2f", item.GetCost())))
	if prev := item.GetPreviousCost(); prev != nil {
		fmt.Fprintf(&b, "%s %s\n", muted.Render("Previous:    "), fmt.Sprintf("$%.2f", *prev))
	}
	if change := item.GetChangePercentage(); change != nil {
		changeStr := formatChangePercentage(change)
		style := lipglossv2.NewStyle().Foreground(changeColor(*change)).Bold(true)
		fmt.Fprintf(&b, "%s %s\n", muted.Render("Change:      "), style.Render(changeStr))
	}

	dims := detailDimensions(item)
	if len(dims) > 0 {
		b.WriteString("\n")
		b.WriteString(muted.Render("Dimensions:"))
		b.WriteString("\n")
		for _, d := range dims {
			fmt.Fprintf(&b, "  %s %s\n", muted.Render(d.label+":"), d.value)
		}
	}

	if pts := item.GetSpendingsByDate(); len(pts) > 0 {
		b.WriteString("\n")
		b.WriteString(muted.Render(fmt.Sprintf("Timeline (%d days):", len(pts))))
		b.WriteString("\n  ")
		b.WriteString(primary.Render(renderSparkline(pts, m.width-8)))
		b.WriteString("\n")
		b.WriteString("  " + muted.Render(formatSparklineRange(pts)))
		b.WriteString("\n")
	}

	return b.String()
}

func (m costsModel) renderFilterDrawer() string {
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	primary := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)
	accent := lipglossv2.NewStyle().Foreground(output.BrandAccent).Bold(true)

	var b strings.Builder
	b.WriteString(accent.Render(fmt.Sprintf("Filter by: %s", costsFilterDims[m.filterDim].label)))
	b.WriteString("\n")
	b.WriteString(primary.Render("  ") + m.filterInput.View())
	b.WriteString("\n")
	if active := activeFilterSummary(m.baseState); active != "" {
		b.WriteString(muted.Render("  Active: " + active))
		b.WriteString("\n")
	}

	border := lipglossv2.NewStyle().
		Border(lipglossv2.RoundedBorder()).
		BorderForeground(output.BrandAccent).
		Width(m.width - 2)
	return border.Render(b.String())
}

func (m costsModel) renderCostsHelp() string {
	title := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true).Render("Keyboard Shortcuts")
	m.help.ShowAll = true
	content := m.help.View(&costsKeys)
	m.help.ShowAll = false

	block := title + "\n\n" + content

	availHeight := m.height - m.kpiHeaderHeight() - 2
	if availHeight < 1 {
		availHeight = 1
	}
	return lipglossv2.Place(m.width, availHeight, lipglossv2.Center, lipglossv2.Center, block)
}

func (m costsModel) costsSortArrow() string {
	if m.sortOrder == sortOrderDesc {
		return arrowDown
	}
	return arrowUp
}

func (m costsModel) fetchCostsPage() tea.Cmd {
	client := m.client
	providerID := m.providerID
	page := m.page
	pageSize := m.pageSize
	sortBy := costsSortableColumns[m.sortIdx].key
	sortOrder := m.sortOrder
	base := m.baseState

	return func() tea.Msg {
		state := base
		state.page = page
		state.pageSize = pageSize
		state.sortBy = sortBy
		state.sortOrder = sortOrder
		state.format = "table"

		req := buildCostsListRequest(state)
		req.Format = api.StringPtr("table")

		resp, err := client.SDK().Costs.ListByProvider(context.Background(), providerID, req)
		if err != nil {
			return costsListFetchMsg{err: err}
		}
		tableResp := resp.GetProviderServiceBreakdownResponse()
		if tableResp == nil || tableResp.GetData() == nil {
			return costsListFetchMsg{err: fmt.Errorf("unexpected response shape")}
		}
		data := tableResp.GetData()
		return costsListFetchMsg{
			data:       data,
			items:      data.GetItems(),
			pagination: data.GetPagination(),
		}
	}
}

type detailDim struct{ label, value string }

func detailDimensions(item *levelfourgo.ProviderServiceBreakdownItem) []detailDim {
	var out []detailDim
	if s := item.GetService(); s != nil && *s != "" {
		out = append(out, detailDim{columnService, *s})
	}
	if r := item.GetRegion(); r != nil && *r != "" {
		out = append(out, detailDim{"Region", *r})
	}
	if a := item.GetAccountID(); a != nil && *a != "" {
		out = append(out, detailDim{columnAccount, *a})
	}
	if e := item.GetEnvironment(); e != nil && *e != "" {
		out = append(out, detailDim{"Environment", *e})
	}
	if k := item.GetTagKey(); k != nil && *k != "" {
		out = append(out, detailDim{"Tag Key", *k})
	}
	if v := item.GetTagValue(); v != nil && *v != "" {
		out = append(out, detailDim{"Tag Value", *v})
	}
	return out
}

func formatDetailHeader(item *levelfourgo.ProviderServiceBreakdownItem) string {
	parts := []string{}
	if s := item.GetService(); s != nil && *s != "" {
		parts = append(parts, *s)
	}
	if r := item.GetRegion(); r != nil && *r != "" {
		parts = append(parts, *r)
	}
	if a := item.GetAccountID(); a != nil && *a != "" {
		parts = append(parts, *a)
	}
	if k := item.GetTagKey(); k != nil && *k != "" {
		tv := ""
		if v := item.GetTagValue(); v != nil {
			tv = *v
		}
		parts = append(parts, fmt.Sprintf("%s=%s", *k, tv))
	}
	if len(parts) == 0 {
		return "Cost breakdown"
	}
	return strings.Join(parts, "  /  ")
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func renderSparkline(points []*levelfourgo.SpendingByDateItem, maxWidth int) string {
	if len(points) == 0 {
		return ""
	}
	if maxWidth < 1 {
		maxWidth = 1
	}

	values := make([]float64, len(points))
	for i, p := range points {
		values[i] = p.GetValue()
	}

	values = bucketValues(values, maxWidth)

	maxV := 0.0
	for _, v := range values {
		if v > maxV {
			maxV = v
		}
	}
	if maxV == 0 {
		return strings.Repeat(string(sparkBlocks[0]), len(values))
	}

	var b strings.Builder
	for _, v := range values {
		idx := int(math.Round((v / maxV) * float64(len(sparkBlocks)-1)))
		if idx < 0 {
			idx = 0
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}

func bucketValues(values []float64, maxWidth int) []float64 {
	if len(values) <= maxWidth {
		return values
	}
	out := make([]float64, maxWidth)
	for i := 0; i < maxWidth; i++ {
		lo := i * len(values) / maxWidth
		hi := (i + 1) * len(values) / maxWidth
		sum := 0.0
		for _, v := range values[lo:hi] {
			sum += v
		}
		out[i] = sum / float64(hi-lo)
	}
	return out
}

func formatSparklineRange(points []*levelfourgo.SpendingByDateItem) string {
	if len(points) == 0 {
		return ""
	}
	first := points[0]
	last := points[len(points)-1]
	minV, maxV := first.GetValue(), first.GetValue()
	for _, p := range points {
		v := p.GetValue()
		if v < minV {
			minV = v
		}
		if v > maxV {
			maxV = v
		}
	}
	return fmt.Sprintf("%s ($%.2f) → %s ($%.2f) · min $%.2f · max $%.2f",
		first.GetDate(), first.GetValue(), last.GetDate(), last.GetValue(), minV, maxV)
}

func changeColor(v float64) color.Color {
	switch {
	case v > 5:
		return output.BrandError
	case v < -5:
		return output.BrandSuccess
	default:
		return output.BrandMuted
	}
}

var runCostsTUI = func(
	client *api.SDKClient,
	providerID string,
	data *levelfourgo.ProviderServiceBreakdownData,
	items []*levelfourgo.ProviderServiceBreakdownItem,
	pagination *levelfourgo.PaginationMeta,
	pageSize int,
	base costsFilterState,
) error {
	m := newCostsModel(client, providerID, data, items, pagination, pageSize, base)

	tty, err := os.Open("/dev/tty")
	if err != nil {
		_, runErr := tea.NewProgram(m).Run()
		return runErr
	}
	defer func() { _ = tty.Close() }()

	w, h := 80, 24
	if ww, hh, sizeErr := term.GetSize(int(tty.Fd())); sizeErr == nil {
		w, h = ww, hh
	}

	m.width = w
	m.height = h
	m.table = m.buildTable()
	m.detail = m.buildDetail()
	m.ready = true

	opts := []tea.ProgramOption{
		tea.WithInput(tty),
		tea.WithColorProfile(colorprofile.ANSI256),
		tea.WithFilter(tuicommon.FilterTerminalNoise),
		tea.WithWindowSize(w, h),
	}

	_, runErr := tea.NewProgram(m, opts...).Run()
	return runErr
}
