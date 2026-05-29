package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/template"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/itchyny/gojq"
	"golang.org/x/term"
)

var (
	JSONMode     bool
	JQExpression string
	TemplateFmt  string
	CSVMode      bool
	QuietMode    bool
	NoColor      bool
	Stdout       io.Writer = os.Stdout
	Stderr       io.Writer = os.Stderr
)

var (
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("167"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("173"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
	borderStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	MutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	AccentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("159"))
)

func PrintJSON(v interface{}) {
	if QuietMode {
		return
	}
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(Stdout, string(data))
}

func PrintResult(v interface{}) error {
	if TemplateFmt != "" {
		return printTemplate(v)
	}
	if JQExpression != "" {
		return printJQ(v)
	}
	if JSONMode {
		PrintJSON(v)
		return nil
	}
	return nil
}

func printJQ(v interface{}) error {
	query, err := gojq.Parse(JQExpression)
	if err != nil {
		return fmt.Errorf("invalid jq expression: %w", err)
	}

	data, _ := json.Marshal(v)
	var input interface{}
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("failed to parse data for jq: %w", err)
	}

	iter := query.Run(input)
	for {
		val, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := val.(error); isErr {
			return fmt.Errorf("jq error: %w", err)
		}
		out, _ := json.MarshalIndent(val, "", "  ")
		fmt.Fprintln(Stdout, string(out))
	}
	return nil
}

func printTemplate(v interface{}) error {
	tmpl, err := template.New("output").Parse(TemplateFmt)
	if err != nil {
		return fmt.Errorf("invalid template: %w", err)
	}

	data, _ := json.Marshal(v)
	var input interface{}
	if err := json.Unmarshal(data, &input); err != nil {
		return fmt.Errorf("failed to parse data for template: %w", err)
	}

	return tmpl.Execute(Stdout, input)
}

type KPICard struct {
	Label string
	Value string
}

func KPICards(cards []KPICard) {
	if QuietMode {
		return
	}

	width := getTermWidth()

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	valueStyle := lipgloss.NewStyle().Bold(true)

	cardGap := 2
	minCardWidth := 20
	maxCardsPerRow := len(cards)
	availWidth := width - (cardGap * (maxCardsPerRow - 1))
	cardWidth := availWidth / maxCardsPerRow
	if cardWidth < minCardWidth {
		maxCardsPerRow = 2
		if maxCardsPerRow > len(cards) {
			maxCardsPerRow = len(cards)
		}
		availWidth = width - (cardGap * (maxCardsPerRow - 1))
		cardWidth = availWidth / maxCardsPerRow
		if cardWidth < minCardWidth {
			cardWidth = minCardWidth
		}
	}

	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("242")).
		Padding(0, 1).
		Width(cardWidth - 2)

	rendered := make([]string, len(cards))
	for i, c := range cards {
		content := labelStyle.Render(c.Label) + "\n" + valueStyle.Render(c.Value)
		rendered[i] = cardStyle.Render(content)
	}

	var rows []string
	for i := 0; i < len(rendered); i += maxCardsPerRow {
		end := i + maxCardsPerRow
		if end > len(rendered) {
			end = len(rendered)
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top, rendered[i:end]...)
		rows = append(rows, row)
	}

	fmt.Fprintln(Stdout, lipgloss.JoinVertical(lipgloss.Left, rows...))
	fmt.Fprintln(Stdout)
}

var termGetSize = term.GetSize

var getTermWidth = func() int {
	if f, ok := Stdout.(*os.File); ok {
		if w, _, err := termGetSize(safeIntFd(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

func PaginationFooter(current, total, totalItems int, hasNext bool) {
	if QuietMode {
		return
	}
	line := fmt.Sprintf("  Page %d of %d (%d total)", current, total, totalItems)
	fmt.Fprintln(Stdout, MutedStyle.Render(line))
	if hasNext {
		fmt.Fprintln(Stdout, MutedStyle.Render(fmt.Sprintf("  Use --page %d to see more results.", current+1)))
	}
}

var statusColors = map[string]lipgloss.Color{
	"active":      lipgloss.Color("114"),
	"optimized":   lipgloss.Color("114"),
	"complete":    lipgloss.Color("114"),
	"available":   lipgloss.Color("75"),
	"failed":      lipgloss.Color("167"),
	"error":       lipgloss.Color("167"),
	"rejected":    lipgloss.Color("167"),
	"unavailable": lipgloss.Color("242"),
	"pending":     lipgloss.Color("173"),
	"processing":  lipgloss.Color("173"),
	"in_review":   lipgloss.Color("141"),
}

func savingsPercentStyle(cell string, base lipgloss.Style) lipgloss.Style {
	trimmed := strings.TrimSuffix(cell, "%")
	trimmed = strings.TrimPrefix(trimmed, "-")
	var pct float64
	if _, err := fmt.Sscanf(trimmed, "%f", &pct); err == nil {
		if pct >= 50 {
			return base.Foreground(lipgloss.Color("114"))
		}
		if pct >= 20 {
			return base.Foreground(lipgloss.Color("173"))
		}
	}
	return base
}

func Table(headers []string, rows [][]string) {
	TableTo(Stdout, headers, rows)
}

func TableTo(w io.Writer, headers []string, rows [][]string) {
	if QuietMode {
		return
	}

	savingsColIdx := -1
	for i, h := range headers {
		if h == "Savings %" {
			savingsColIdx = i
		}
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("75"))
			}
			s := lipgloss.NewStyle()
			if row >= 0 && row < len(rows) {
				if row%2 == 1 {
					s = s.Foreground(lipgloss.Color("252"))
				}
				cell := rows[row][col]
				if c, ok := statusColors[strings.ToLower(cell)]; ok {
					s = s.Foreground(c).Bold(true)
				}
				if col == savingsColIdx && len(cell) > 0 {
					s = savingsPercentStyle(cell, s)
				}
				if len(cell) > 0 && (cell[0] == '$' || cell[0] == '-' || cell[0] == '+') {
					s = s.Align(lipgloss.Right)
				}
			}
			return s
		}).
		Headers(headers...).
		Rows(rows...)

	fmt.Fprintln(w, t)
}

func Badge(label string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("231")).
		Background(color).
		Padding(0, 1).
		Render(label)
}

func Header(title string) {
	if QuietMode {
		return
	}
	fmt.Fprintln(Stdout, boldStyle.Render(title))
}

func KeyValue(key, value string) {
	if QuietMode {
		return
	}
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Bold(true).Render(key + ":")
	fmt.Fprintf(Stdout, "  %s %s\n", label, value)
}

func StatusBadge(status string) string {
	if c, ok := statusColors[strings.ToLower(status)]; ok {
		return lipgloss.NewStyle().Foreground(c).Bold(true).Render(status)
	}
	return status
}

func Error(msg string) {
	if QuietMode {
		return
	}
	fmt.Fprintf(Stderr, "%s %s\n", errorStyle.Render("Error:"), msg)
}

func Success(msg string) {
	if QuietMode {
		return
	}
	fmt.Fprintln(Stdout, successStyle.Render("\u2713 "+msg))
}

func Warning(msg string) {
	if QuietMode {
		return
	}
	fmt.Fprintf(Stderr, "%s %s\n", warnStyle.Render("Warning:"), msg)
}

func Info(msg string) {
	if QuietMode {
		return
	}
	fmt.Fprintln(Stdout, msg)
}

func InfoLabel(msg string) {
	if QuietMode {
		return
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("114")).Render("INFO")
	fmt.Fprintf(Stdout, "%s %s\n", label, msg)
}

func WarnLabel(msg string) {
	if QuietMode {
		return
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("173")).Render("WARNING")
	fmt.Fprintf(Stderr, "%s %s\n", label, msg)
}

func ErrorLabel(msg string) {
	if QuietMode {
		return
	}
	label := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("167")).Render("ERROR")
	fmt.Fprintf(Stderr, "%s %s\n", label, msg)
}

func Bold(s string) string {
	return boldStyle.Render(s)
}

func Muted(s string) string {
	if NoColor {
		return s
	}
	return MutedStyle.Render(s)
}

func Accent(s string) string {
	if NoColor {
		return s
	}
	return AccentStyle.Render(s)
}

func HasFormattingFlags() bool {
	return JSONMode || JQExpression != "" || TemplateFmt != ""
}

func PrintCSV(headers []string, rows [][]string) {
	if QuietMode {
		return
	}
	w := csv.NewWriter(Stdout)
	_ = w.Write(headers)
	for _, row := range rows {
		_ = w.Write(row)
	}
	w.Flush()
}

func PrintRaw(s string) {
	if QuietMode {
		return
	}
	fmt.Fprint(Stdout, s)
}

var IsTTY = func() bool {
	if f, ok := Stdout.(*os.File); ok {
		return term.IsTerminal(safeIntFd(f.Fd()))
	}
	return false
}

func safeIntFd(fd uintptr) int {
	if fd > uintptr(^uint(0)>>1) {
		return -1
	}
	return int(fd)
}

var glamourRender = glamour.RenderWithEnvironmentConfig

func Markdown(content string) {
	if QuietMode {
		return
	}
	rendered, err := glamourRender(content)
	if err != nil {
		fmt.Fprintln(Stdout, content)
		return
	}
	fmt.Fprint(Stdout, rendered)
}
