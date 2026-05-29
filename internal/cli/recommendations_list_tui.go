package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	btable "charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lipglossv2 "charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

type listKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	NextPage  key.Binding
	PrevPage  key.Binding
	Sort      key.Binding
	SortOrder key.Binding
	Search    key.Binding
	Open      key.Binding
	Help      key.Binding
	Quit      key.Binding
	Cancel    key.Binding
	Confirm   key.Binding
}

var listKeys = listKeyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Enter:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view detail")),
	NextPage:  key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")),
	PrevPage:  key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prev page")),
	Sort:      key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "sort column")),
	SortOrder: key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "sort order")),
	Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Open:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open in browser")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:      key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	Confirm:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "confirm")),
}

func (k listKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Search, k.Sort, k.NextPage, k.PrevPage, k.Help, k.Quit}
}

func (k listKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Search, k.Sort, k.SortOrder},
		{k.NextPage, k.PrevPage, k.Open},
		{k.Help, k.Quit},
	}
}

var sortableColumns = []struct {
	label string
	key   string
}{
	{"savings $", "monthly_savings"},
	{"savings %", "savings_percentage"},
	{"status", "status"},
	{"service", "service"},
	{"environment", "environment"},
	{"id", "recommendation_id"},
}

type listFetchMsg struct {
	items      []*levelfourgo.ProviderBreakdownItem
	pagination *levelfourgo.PaginationMeta
	err        error
}

const (
	sortOrderAsc  = "asc"
	sortOrderDesc = "desc"
	arrowUp       = "↑"
	arrowDown     = "↓"
	dashSymbol    = "—"
	columnAccount = "Account"
	columnService = "Service"
)

type recommendationsListModel struct {
	items      []*levelfourgo.ProviderBreakdownItem
	allItems   []*levelfourgo.ProviderBreakdownItem
	overview   *levelfourgo.RecommendationsOverviewData
	providerID string

	table btable.Model

	pagination *levelfourgo.PaginationMeta
	page       int
	pageSize   int

	mode        tuicommon.Mode
	searchInput textinput.Model
	searchQuery string

	sortIdx   int
	sortOrder string

	width   int
	height  int
	ready   bool
	loading bool

	help     help.Model
	spinner  spinner.Model
	feedback string

	client   *api.SDKClient
	selected string
}

func newRecommendationsListModel(
	client *api.SDKClient,
	providerID string,
	overview *levelfourgo.RecommendationsOverviewData,
	items []*levelfourgo.ProviderBreakdownItem,
	pagination *levelfourgo.PaginationMeta,
	pageSize int,
) recommendationsListModel {
	si := textinput.New()
	si.Prompt = "/ "
	si.Placeholder = "search..."

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

	return recommendationsListModel{
		items:       items,
		allItems:    items,
		overview:    overview,
		providerID:  providerID,
		pagination:  pagination,
		page:        pg,
		pageSize:    pageSize,
		searchInput: si,
		sortIdx:     0,
		sortOrder:   sortOrderDesc,
		help:        h,
		spinner:     sp,
		client:      client,
	}
}

func (m recommendationsListModel) Init() tea.Cmd {
	if m.ready {
		return nil
	}
	return m.spinner.Tick
}

func (m recommendationsListModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table = m.buildTable()
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

	case listFetchMsg:
		m.loading = false
		if msg.err != nil {
			m.feedback = fmt.Sprintf("Error: %v", msg.err)
			return m, nil
		}
		m.allItems = msg.items
		m.items = msg.items
		m.pagination = msg.pagination
		if msg.pagination != nil {
			m.page = msg.pagination.GetCurrentPage()
		}
		m.searchQuery = ""
		m.searchInput.SetValue("")
		m.table = m.buildTable()
		return m, nil

	case tea.KeyPressMsg:
		switch m.mode {
		case tuicommon.ModeHelp:
			return m.updateListHelp(msg)
		case tuicommon.ModeSearch:
			return m.updateListSearch(msg)
		default:
			return m.updateListNormal(msg)
		}
	}

	if m.ready {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m recommendationsListModel) updateListNormal(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, listKeys.Quit):
		return m, tea.Quit

	case key.Matches(msg, listKeys.Enter):
		row := m.table.SelectedRow()
		if len(row) > 0 {
			m.selected = row[0]
		}
		return m, tea.Quit

	case key.Matches(msg, listKeys.Search):
		m.mode = tuicommon.ModeSearch
		m.searchInput.Focus()
		return m, nil

	case key.Matches(msg, listKeys.Help):
		m.mode = tuicommon.ModeHelp
		return m, nil

	case key.Matches(msg, listKeys.Sort), key.Matches(msg, listKeys.SortOrder):
		return m.handleSort(msg)

	case key.Matches(msg, listKeys.NextPage), key.Matches(msg, listKeys.PrevPage):
		return m.handlePageNav(msg)

	case key.Matches(msg, listKeys.Open):
		_ = openWeb("/savings-recommendations")
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m recommendationsListModel) handleSort(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, listKeys.Sort) {
		m.sortIdx = (m.sortIdx + 1) % len(sortableColumns)
	} else {
		if m.sortOrder == sortOrderDesc {
			m.sortOrder = sortOrderAsc
		} else {
			m.sortOrder = sortOrderDesc
		}
	}
	m.feedback = fmt.Sprintf("Sort: %s %s", sortableColumns[m.sortIdx].label, m.sortArrow())
	return m, m.fetchCurrentPage()
}

func (m recommendationsListModel) handlePageNav(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	r := tuicommon.ResolvePageNav(msg, m.page, m.pagination, listKeys.NextPage, listKeys.PrevPage)
	if !r.Changed {
		return m, nil
	}
	m.page = r.Page
	m.loading = true
	return m, tea.Batch(m.fetchCurrentPage(), m.spinner.Tick)
}

func (m recommendationsListModel) updateListSearch(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	action := tuicommon.ResolveSearchAction(msg, listKeys.Cancel, listKeys.Confirm)

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

func (m recommendationsListModel) updateListHelp(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, listKeys.Help), key.Matches(msg, listKeys.Cancel):
		m.mode = tuicommon.ModeNormal
		return m, nil
	case key.Matches(msg, listKeys.Quit):
		return m, tea.Quit
	}
	return m, nil
}

func (m *recommendationsListModel) applySearch() {
	if m.searchQuery == "" {
		m.items = m.allItems
	} else {
		query := strings.ToLower(m.searchQuery)
		var filtered []*levelfourgo.ProviderBreakdownItem
		for _, item := range m.allItems {
			if m.itemMatchesSearch(item, query) {
				filtered = append(filtered, item)
			}
		}
		m.items = filtered
	}
	m.rebuildTableRows()
}

func (m *recommendationsListModel) itemMatchesSearch(item *levelfourgo.ProviderBreakdownItem, query string) bool {
	fields := []string{
		item.GetRecommendationID(),
		item.GetService(),
		fmt.Sprintf("$%.2f", item.GetMonthlySavings()),
	}
	if e := item.GetEnvironment(); e != nil {
		fields = append(fields, *e)
	}
	if a := item.GetAccount(); a != nil {
		fields = append(fields, *a)
	}
	if t := item.GetTag(); t != nil {
		fields = append(fields, *t)
	}
	if s := item.GetStatus(); s != nil {
		fields = append(fields, string(*s))
	}
	if a := item.GetSavingAcceptedBy(); a != nil {
		fields = append(fields, *a)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), query) {
			return true
		}
	}
	return false
}

func (m *recommendationsListModel) rebuildTableRows() {
	rows := m.buildRows()
	m.table.SetRows(rows)
}

func (m recommendationsListModel) buildTable() btable.Model {
	cols := m.buildColumns()
	rows := m.buildRows()

	s := btable.Styles{
		Header: lipglossv2.NewStyle().Bold(true).Foreground(output.BrandPrimary),
		Cell:   lipglossv2.NewStyle(),
		Selected: lipglossv2.NewStyle().
			Bold(true).
			Foreground(lipglossv2.ANSIColor(231)).
			Background(output.BrandPrimary),
	}

	tableHeight := m.height - m.headerHeight() - 2
	if tableHeight < 3 {
		tableHeight = 3
	}

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

func (m recommendationsListModel) buildColumns() []btable.Column {
	w := m.width
	type colDef struct {
		title    string
		minWidth int
		weight   int
	}
	defs := []colDef{
		{"ID", 0, 3},
		{"Service", 0, 2},
		{"Savings $", 0, 2},
		{"Status", 0, 2},
		{"Environment", 100, 2},
		{"Savings %", 100, 2},
		{"Account", 140, 2},
		{"Tag", 160, 2},
		{"Author", 160, 3},
	}

	var active []colDef
	for _, d := range defs {
		if w >= d.minWidth {
			active = append(active, d)
		}
	}

	totalWeight := 0
	for _, d := range active {
		totalWeight += d.weight
	}

	usable := w - len(active) - 1
	if usable < len(active)*5 {
		usable = len(active) * 5
	}

	cols := make([]btable.Column, len(active))
	for i, d := range active {
		cw := (usable * d.weight) / totalWeight
		if cw < 5 {
			cw = 5
		}
		cols[i] = btable.Column{Title: d.title, Width: cw}
	}
	return cols
}

func (m recommendationsListModel) buildRows() []btable.Row {
	w := m.width
	var rows []btable.Row
	for _, item := range m.items {
		var row btable.Row
		row = append(row, item.GetRecommendationID())
		row = append(row, item.GetService())
		row = append(row, fmt.Sprintf("$%.2f", item.GetMonthlySavings()))
		status := ""
		if s := item.GetStatus(); s != nil {
			status = string(*s)
		}
		row = append(row, status)

		if w >= 100 {
			env := ""
			if e := item.GetEnvironment(); e != nil {
				env = *e
			}
			row = append(row, env)
			row = append(row, fmt.Sprintf("%.1f%%", item.GetSavingsPercentage()))
		}
		if w >= 140 {
			account := ""
			if a := item.GetAccount(); a != nil {
				account = *a
			}
			row = append(row, account)
		}
		if w >= 160 {
			tag := ""
			if t := item.GetTag(); t != nil {
				tag = *t
			}
			row = append(row, tag)
			author := ""
			if a := item.GetSavingAcceptedBy(); a != nil {
				author = *a
			}
			row = append(row, author)
		}
		rows = append(rows, row)
	}
	return rows
}

func (m recommendationsListModel) headerHeight() int {
	return 6
}

func (m recommendationsListModel) View() tea.View {
	if !m.ready {
		lv := tea.NewView(fmt.Sprintf("\n  %s Loading recommendations...\n", m.spinner.View()))
		lv.AltScreen = true
		return lv
	}

	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	primary := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true)

	var b strings.Builder

	b.WriteString(m.renderKPIHeader())
	b.WriteString("\n")

	switch {
	case m.loading:
		fmt.Fprintf(&b, "  %s Loading...\n", m.spinner.View())
	case m.mode == tuicommon.ModeHelp:
		b.WriteString(m.renderListHelp())
	default:
		b.WriteString(m.table.View())
		b.WriteString("\n")
	}

	var footer strings.Builder
	if m.mode == tuicommon.ModeSearch {
		footer.WriteString("  " + m.searchInput.View())
		if m.searchQuery != "" {
			footer.WriteString(muted.Render(fmt.Sprintf("  [%d matches]", len(m.items))))
		}
	} else {
		if m.pagination != nil {
			footer.WriteString(muted.Render(fmt.Sprintf("  Page %d/%d (%d items)",
				m.page, m.pagination.GetTotalPages(), m.pagination.GetTotalItems())))
		}
		footer.WriteString(muted.Render("  │  "))
		footer.WriteString(primary.Render(fmt.Sprintf("Sort: %s %s", sortableColumns[m.sortIdx].label, m.sortArrow())))
		if m.feedback != "" {
			footer.WriteString(muted.Render("  │  "))
			footer.WriteString(lipglossv2.NewStyle().Foreground(output.BrandSuccess).Render(m.feedback))
		}
		footer.WriteString(muted.Render("  │  "))
		footer.WriteString(muted.Render(m.help.View(&listKeys)))
	}
	b.WriteString(footer.String())

	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m recommendationsListModel) renderKPIHeader() string {
	if m.overview == nil {
		return ""
	}
	muted := lipglossv2.NewStyle().Foreground(output.BrandMuted)
	bold := lipglossv2.NewStyle().Bold(true)

	cardWidth := (m.width - 12) / 4
	if cardWidth < 18 {
		cardWidth = 18
	}

	cards := []struct{ label, value string }{
		{"Total Spend", fmt.Sprintf("$%.2f/mo", m.overview.GetTotalSpend())},
		{"Available Savings", fmt.Sprintf("$%.2f/mo", m.overview.GetAvailableSavings())},
		{"Pending Savings", fmt.Sprintf("$%.2f/mo", m.overview.GetPendingSavings())},
		{"Saved CTD", fmt.Sprintf("$%.2f/mo", m.overview.GetSavedItd())},
	}

	var rendered []string
	for _, c := range cards {
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

func (m recommendationsListModel) renderListHelp() string {
	title := lipglossv2.NewStyle().Foreground(output.BrandPrimary).Bold(true).Render("Keyboard Shortcuts")
	m.help.ShowAll = true
	content := m.help.View(&listKeys)
	m.help.ShowAll = false

	block := title + "\n\n" + content

	availHeight := m.height - m.headerHeight() - 2
	if availHeight < 1 {
		availHeight = 1
	}
	return lipglossv2.Place(m.width, availHeight, lipglossv2.Center, lipglossv2.Center, block)
}

func (m recommendationsListModel) sortArrow() string {
	if m.sortOrder == sortOrderDesc {
		return arrowDown
	}
	return arrowUp
}

func (m recommendationsListModel) fetchCurrentPage() tea.Cmd {
	client := m.client
	providerID := m.providerID
	page := m.page
	pageSize := m.pageSize
	sortBy := sortableColumns[m.sortIdx].key
	sortOrder := m.sortOrder

	return func() tea.Msg {
		req := buildListByProviderRequest(page, pageSize)
		req.SortBy = api.StringPtr(sortBy)
		so := levelfourgo.ListByProviderRecommendationsRequestSortOrder(sortOrder)
		req.SortOrder = &so

		pg, err := client.SDK().Recommendations.ListByProvider(context.Background(), providerID, req)
		if err != nil {
			return listFetchMsg{err: err}
		}
		return listFetchMsg{
			items:      pg.Results,
			pagination: pg.Response.GetData().GetPagination(),
		}
	}
}

var runRecommendationsListTUI = func(
	client *api.SDKClient,
	providerID string,
	overview *levelfourgo.RecommendationsOverviewData,
	items []*levelfourgo.ProviderBreakdownItem,
	pagination *levelfourgo.PaginationMeta,
	pageSize int,
) (string, error) {
	m := newRecommendationsListModel(client, providerID, overview, items, pagination, pageSize)

	tty, err := os.Open("/dev/tty")
	if err != nil {
		p, runErr := tea.NewProgram(m).Run()
		if runErr != nil {
			return "", runErr
		}
		if final, ok := p.(recommendationsListModel); ok && final.selected != "" {
			return final.selected, nil
		}
		return "", nil
	}
	defer func() { _ = tty.Close() }()

	w, h := 80, 24
	if ww, hh, sizeErr := term.GetSize(int(tty.Fd())); sizeErr == nil {
		w, h = ww, hh
	}

	m.width = w
	m.height = h
	m.table = m.buildTable()
	m.ready = true

	opts := []tea.ProgramOption{
		tea.WithInput(tty),
		tea.WithColorProfile(colorprofile.ANSI256),
		tea.WithFilter(tuicommon.FilterTerminalNoise),
		tea.WithWindowSize(w, h),
	}

	p, runErr := tea.NewProgram(m, opts...).Run()
	if runErr != nil {
		return "", runErr
	}
	if final, ok := p.(recommendationsListModel); ok && final.selected != "" {
		return final.selected, nil
	}
	return "", nil
}
