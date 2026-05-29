package output

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func setupOutput(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	origOut, origErr := Stdout, Stderr
	origJSON, origJQ, origTmpl := JSONMode, JQExpression, TemplateFmt
	Stdout = &outBuf
	Stderr = &errBuf
	t.Cleanup(func() {
		Stdout = origOut
		Stderr = origErr
		JSONMode = origJSON
		JQExpression = origJQ
		TemplateFmt = origTmpl
	})
	return &outBuf, &errBuf
}

func TestPrintJSON(t *testing.T) {
	outBuf, _ := setupOutput(t)

	PrintJSON(map[string]string{"key": "value"})
	got := outBuf.String()
	if !strings.Contains(got, `"key": "value"`) {
		t.Errorf("PrintJSON output = %q, want JSON with key:value", got)
	}
}

func TestPrintResultJSONMode(t *testing.T) {
	outBuf, _ := setupOutput(t)
	JSONMode = true

	err := PrintResult(map[string]string{"mode": "json"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), `"mode": "json"`) {
		t.Errorf("expected JSON output, got %q", outBuf.String())
	}
}

func TestPrintResultJQMode(t *testing.T) {
	outBuf, _ := setupOutput(t)
	JQExpression = ".name"

	err := PrintResult(map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), `"test"`) {
		t.Errorf("expected jq output, got %q", outBuf.String())
	}
}

func TestPrintResultTemplateMode(t *testing.T) {
	outBuf, _ := setupOutput(t)
	TemplateFmt = "Name: {{.name}}"

	err := PrintResult(map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(outBuf.String(), "Name: test") {
		t.Errorf("expected template output, got %q", outBuf.String())
	}
}

func TestPrintResultNoMode(t *testing.T) {
	outBuf, _ := setupOutput(t)
	JSONMode = false
	JQExpression = ""
	TemplateFmt = ""

	err := PrintResult(map[string]string{"key": "value"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outBuf.String() != "" {
		t.Errorf("expected no output, got %q", outBuf.String())
	}
}

func TestPrintJQInvalidExpression(t *testing.T) {
	setupOutput(t)
	JQExpression = ".[invalid"

	err := printJQ(map[string]string{"key": "value"})
	if err == nil {
		t.Error("expected error for invalid jq expression")
	}
}

func TestPrintJQRuntimeError(t *testing.T) {
	setupOutput(t)

	JQExpression = ".foo | keys"
	err := printJQ(map[string]interface{}{"foo": "not_an_object"})
	if err == nil {
		t.Error("expected error for jq runtime error")
	}
}

func TestPrintJQUnmarshalError(t *testing.T) {
	setupOutput(t)
	JQExpression = "."

	err := printJQ(make(chan int))
	if err == nil {
		t.Error("expected error for unmarshalable value")
	}
}

func TestPrintTemplateUnmarshalError(t *testing.T) {
	setupOutput(t)
	TemplateFmt = "{{.}}"

	err := printTemplate(make(chan int))
	if err == nil {
		t.Error("expected error for unmarshalable value")
	}
}

func TestPrintTemplateInvalid(t *testing.T) {
	setupOutput(t)
	TemplateFmt = "{{.bad"

	err := printTemplate(map[string]string{"key": "value"})
	if err == nil {
		t.Error("expected error for invalid template")
	}
}

func TestTable(t *testing.T) {
	outBuf, _ := setupOutput(t)

	Table([]string{"Name", "Value"}, [][]string{{"foo", "bar"}})
	got := outBuf.String()
	if !strings.Contains(got, "foo") || !strings.Contains(got, "bar") {
		t.Errorf("Table output missing data: %q", got)
	}
}

func TestError(t *testing.T) {
	_, errBuf := setupOutput(t)

	Error("something failed")
	if !strings.Contains(errBuf.String(), "something failed") {
		t.Errorf("Error output = %q, want 'something failed'", errBuf.String())
	}
}

func TestSuccess(t *testing.T) {
	outBuf, _ := setupOutput(t)

	Success("it worked")
	if !strings.Contains(outBuf.String(), "it worked") {
		t.Errorf("Success output = %q, want 'it worked'", outBuf.String())
	}
}

func TestWarning(t *testing.T) {
	_, errBuf := setupOutput(t)

	Warning("be careful")
	if !strings.Contains(errBuf.String(), "be careful") {
		t.Errorf("Warning output = %q, want 'be careful'", errBuf.String())
	}
}

func TestInfo(t *testing.T) {
	outBuf, _ := setupOutput(t)

	Info("some info")
	if !strings.Contains(outBuf.String(), "some info") {
		t.Errorf("Info output = %q, want 'some info'", outBuf.String())
	}
}

func TestBold(t *testing.T) {
	got := Bold("text")
	if !strings.Contains(got, "text") {
		t.Errorf("Bold(%q) = %q, want text content", "text", got)
	}
}

func TestMuted(t *testing.T) {
	origNoColor := NoColor
	t.Cleanup(func() { NoColor = origNoColor })

	NoColor = false
	got := Muted("dim")
	if !strings.Contains(got, "dim") {
		t.Errorf("Muted(%q) = %q, want text content", "dim", got)
	}

	NoColor = true
	got = Muted("dim")
	if got != "dim" {
		t.Errorf("Muted with NoColor = %q, want plain %q", got, "dim")
	}
}

func TestAccent(t *testing.T) {
	origNoColor := NoColor
	t.Cleanup(func() { NoColor = origNoColor })

	NoColor = false
	got := Accent("bright")
	if !strings.Contains(got, "bright") {
		t.Errorf("Accent(%q) = %q, want text content", "bright", got)
	}

	NoColor = true
	got = Accent("bright")
	if got != "bright" {
		t.Errorf("Accent with NoColor = %q, want plain %q", got, "bright")
	}
}

func TestHasFormattingFlags(t *testing.T) {
	origJSON, origJQ, origTmpl := JSONMode, JQExpression, TemplateFmt
	defer func() {
		JSONMode = origJSON
		JQExpression = origJQ
		TemplateFmt = origTmpl
	}()

	JSONMode = false
	JQExpression = ""
	TemplateFmt = ""
	if HasFormattingFlags() {
		t.Error("expected false when no flags set")
	}

	JSONMode = true
	if !HasFormattingFlags() {
		t.Error("expected true when JSONMode set")
	}

	JSONMode = false
	JQExpression = ".foo"
	if !HasFormattingFlags() {
		t.Error("expected true when JQExpression set")
	}

	JQExpression = ""
	TemplateFmt = "{{.foo}}"
	if !HasFormattingFlags() {
		t.Error("expected true when TemplateFmt set")
	}
}

func TestStdoutDefaultsToOsStdout(t *testing.T) {
	origOut := Stdout
	Stdout = os.Stdout
	defer func() { Stdout = origOut }()

	if Stdout != os.Stdout {
		t.Error("expected Stdout to default to os.Stdout")
	}
}

func TestTableStatusColors(t *testing.T) {
	outBuf, _ := setupOutput(t)

	headers := []string{"Name", "Status", "Savings"}
	rows := [][]string{
		{"EC2", "active", "$100.00"},
		{"RDS", "failed", "-$50.00"},
		{"S3", "pending", "+$25.00"},
	}
	Table(headers, rows)
	got := outBuf.String()
	if !strings.Contains(got, "active") || !strings.Contains(got, "failed") || !strings.Contains(got, "pending") {
		t.Errorf("Table output missing status values: %q", got)
	}
}

func TestBadge(t *testing.T) {
	got := Badge("active", "114")
	if !strings.Contains(got, "active") {
		t.Errorf("Badge output missing label: %q", got)
	}
}

func TestHeader(t *testing.T) {
	outBuf, _ := setupOutput(t)
	Header("My Title")
	got := outBuf.String()
	if !strings.Contains(got, "My Title") {
		t.Errorf("Header output = %q, want 'My Title'", got)
	}
}

func TestKeyValue(t *testing.T) {
	outBuf, _ := setupOutput(t)
	KeyValue("Name", "test-value")
	got := outBuf.String()
	if !strings.Contains(got, "Name:") || !strings.Contains(got, "test-value") {
		t.Errorf("KeyValue output = %q, want 'Name:' and 'test-value'", got)
	}
}

func TestStatusBadge(t *testing.T) {
	got := StatusBadge("active")
	if !strings.Contains(got, "active") {
		t.Errorf("StatusBadge output = %q, want 'active'", got)
	}
}

func TestStatusBadgeUnknown(t *testing.T) {
	got := StatusBadge("unknown_status")
	if got != "unknown_status" {
		t.Errorf("StatusBadge unknown = %q, want plain 'unknown_status'", got)
	}
}

func TestMarkdown(t *testing.T) {
	outBuf, _ := setupOutput(t)

	Markdown("# Hello\n\nThis is **bold** text.")
	got := outBuf.String()
	if !strings.Contains(got, "Hello") {
		t.Errorf("Markdown output missing content: %q", got)
	}
}

func TestMarkdownFallback(t *testing.T) {
	outBuf, _ := setupOutput(t)

	origRender := glamourRender
	glamourRender = func(_ string) (string, error) {
		return "", fmt.Errorf("render failed")
	}
	defer func() { glamourRender = origRender }()

	Markdown("raw content")
	got := outBuf.String()
	if !strings.Contains(got, "raw content") {
		t.Errorf("Markdown fallback missing raw content: %q", got)
	}
}

func TestL4SpinnerTheme(t *testing.T) {
	theme := L4SpinnerTheme()
	if theme == nil {
		t.Fatal("L4SpinnerTheme returned nil")
	}
	styles := theme.Theme(true)
	if styles == nil {
		t.Fatal("L4SpinnerTheme.Theme(true) returned nil styles")
	}
}

func TestL4Theme(t *testing.T) {
	theme := L4Theme()
	if theme == nil {
		t.Fatal("L4Theme returned nil")
	}
	styles := theme.Theme(true)
	if styles == nil {
		t.Fatal("L4Theme.Theme(true) returned nil styles")
	}
	stylesLight := theme.Theme(false)
	if stylesLight == nil {
		t.Fatal("L4Theme.Theme(false) returned nil styles")
	}
}

func setupQuiet(t *testing.T) (*bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	outBuf, errBuf := setupOutput(t)
	origQuiet := QuietMode
	QuietMode = true
	t.Cleanup(func() { QuietMode = origQuiet })
	return outBuf, errBuf
}

func TestPrintJSONQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	PrintJSON(map[string]string{"key": "value"})
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestTableQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	Table([]string{"A"}, [][]string{{"B"}})
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestHeaderQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	Header("Title")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestKeyValueQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	KeyValue("K", "V")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestErrorQuiet(t *testing.T) {
	_, errBuf := setupQuiet(t)
	Error("fail")
	if errBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", errBuf.String())
	}
}

func TestSuccessQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	Success("ok")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestWarningQuiet(t *testing.T) {
	_, errBuf := setupQuiet(t)
	Warning("warn")
	if errBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", errBuf.String())
	}
}

func TestInfoQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	Info("msg")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestMarkdownQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	Markdown("# Title")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestPrintCSV(t *testing.T) {
	outBuf, _ := setupOutput(t)
	PrintCSV([]string{"Name", "Value"}, [][]string{{"foo", "bar"}, {"baz", "qux"}})
	got := outBuf.String()
	if !strings.Contains(got, "Name,Value") {
		t.Errorf("PrintCSV missing headers: %q", got)
	}
	if !strings.Contains(got, "foo,bar") {
		t.Errorf("PrintCSV missing row data: %q", got)
	}
	if !strings.Contains(got, "baz,qux") {
		t.Errorf("PrintCSV missing second row: %q", got)
	}
}

func TestPrintCSVQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	PrintCSV([]string{"A"}, [][]string{{"B"}})
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestPrintRaw(t *testing.T) {
	outBuf, _ := setupOutput(t)
	PrintRaw("raw output here")
	if outBuf.String() != "raw output here" {
		t.Errorf("PrintRaw output = %q, want %q", outBuf.String(), "raw output here")
	}
}

func TestPrintRawQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	PrintRaw("raw output here")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestSafeIntFd(t *testing.T) {
	if got := safeIntFd(0); got != 0 {
		t.Errorf("safeIntFd(0) = %d, want 0", got)
	}
	if got := safeIntFd(1); got != 1 {
		t.Errorf("safeIntFd(1) = %d, want 1", got)
	}
	if got := safeIntFd(^uintptr(0)); got != -1 {
		t.Errorf("safeIntFd(maxUintptr) = %d, want -1", got)
	}
}

func TestIsTTYNonFileWriter(t *testing.T) {
	origOut := Stdout
	defer func() { Stdout = origOut }()
	Stdout = &bytes.Buffer{}

	if IsTTY() {
		t.Error("expected IsTTY() = false when Stdout is not *os.File")
	}
}

func TestIsTTYWithFile(t *testing.T) {
	origOut := Stdout
	defer func() { Stdout = origOut }()

	f, err := os.CreateTemp(t.TempDir(), "tty-test")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	Stdout = f

	if IsTTY() {
		t.Error("expected IsTTY() = false for a regular file")
	}
}

func TestInfoLabel(t *testing.T) {
	outBuf, _ := setupOutput(t)
	InfoLabel("Autodetected 1 project")
	got := outBuf.String()
	if !strings.Contains(got, "INFO") || !strings.Contains(got, "Autodetected 1 project") {
		t.Errorf("InfoLabel output = %q, want INFO prefix and message", got)
	}
}

func TestWarnLabel(t *testing.T) {
	_, errBuf := setupOutput(t)
	WarnLabel("something might be wrong")
	got := errBuf.String()
	if !strings.Contains(got, "WARNING") || !strings.Contains(got, "something might be wrong") {
		t.Errorf("WarnLabel output = %q, want WARNING prefix and message", got)
	}
}

func TestErrorLabel(t *testing.T) {
	_, errBuf := setupOutput(t)
	ErrorLabel("something went wrong")
	got := errBuf.String()
	if !strings.Contains(got, "ERROR") || !strings.Contains(got, "something went wrong") {
		t.Errorf("ErrorLabel output = %q, want ERROR prefix and message", got)
	}
}

func TestInfoLabelQuiet(t *testing.T) {
	outBuf, _ := setupQuiet(t)
	InfoLabel("msg")
	if outBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", outBuf.String())
	}
}

func TestWarnLabelQuiet(t *testing.T) {
	_, errBuf := setupQuiet(t)
	WarnLabel("msg")
	if errBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", errBuf.String())
	}
}

func TestErrorLabelQuiet(t *testing.T) {
	_, errBuf := setupQuiet(t)
	ErrorLabel("msg")
	if errBuf.String() != "" {
		t.Errorf("expected no output in quiet mode, got %q", errBuf.String())
	}
}

func TestHasFormattingFlagsCSVMode(t *testing.T) {
	origCSV := CSVMode
	defer func() { CSVMode = origCSV }()

	CSVMode = false
	JSONMode = false
	JQExpression = ""
	TemplateFmt = ""
	if HasFormattingFlags() {
		t.Error("expected false when no flags set")
	}

	CSVMode = true
	if HasFormattingFlags() {
		t.Error("expected false when only CSVMode set (CSVMode no longer triggers HasFormattingFlags)")
	}
}

func TestKPICards(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origWidth := getTermWidth
	getTermWidth = func() int { return 120 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	KPICards([]KPICard{
		{Label: "Total Spend", Value: "$1000.00"},
		{Label: "Savings", Value: "$200.00"},
	})
	got := buf.String()
	if !strings.Contains(got, "Total Spend") {
		t.Errorf("output missing label: %q", got)
	}
	if !strings.Contains(got, "$1000.00") {
		t.Errorf("output missing value: %q", got)
	}
}

func TestKPICardsNarrow(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origWidth := getTermWidth
	getTermWidth = func() int { return 60 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	KPICards([]KPICard{
		{Label: "A", Value: "$100"},
		{Label: "B", Value: "$200"},
		{Label: "C", Value: "$300"},
		{Label: "D", Value: "$400"},
	})
	got := buf.String()
	if !strings.Contains(got, "$100") || !strings.Contains(got, "$400") {
		t.Errorf("narrow KPI cards missing values: %q", got)
	}
}

func TestKPICardsSingleCard(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origWidth := getTermWidth
	getTermWidth = func() int { return 15 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	KPICards([]KPICard{{Label: "Only", Value: "$100"}})
	got := buf.String()
	if !strings.Contains(got, "$100") {
		t.Errorf("single card missing value: %q", got)
	}
}

func TestKPICardsVeryNarrow(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origWidth := getTermWidth
	getTermWidth = func() int { return 30 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	KPICards([]KPICard{
		{Label: "A", Value: "$1"},
		{Label: "B", Value: "$2"},
		{Label: "C", Value: "$3"},
		{Label: "D", Value: "$4"},
	})
	got := buf.String()
	if !strings.Contains(got, "$1") {
		t.Errorf("very narrow cards missing values: %q", got)
	}
}

func TestKPICardsOddCount(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origWidth := getTermWidth
	getTermWidth = func() int { return 50 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	KPICards([]KPICard{
		{Label: "A", Value: "$1"},
		{Label: "B", Value: "$2"},
		{Label: "C", Value: "$3"},
	})
	got := buf.String()
	if !strings.Contains(got, "$3") {
		t.Errorf("odd count cards missing last value: %q", got)
	}
}

func TestKPICardsQuiet(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origQuiet := QuietMode
	QuietMode = true
	defer func() { Stdout = origOut; QuietMode = origQuiet }()

	KPICards([]KPICard{{Label: "Test", Value: "123"}})
	if buf.Len() > 0 {
		t.Error("KPICards should produce no output in quiet mode")
	}
}

func TestPaginationFooter(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	defer func() { Stdout = origOut }()

	PaginationFooter(1, 3, 52, true)
	got := buf.String()
	if !strings.Contains(got, "Page 1 of 3") {
		t.Errorf("missing page info: %q", got)
	}
	if !strings.Contains(got, "52 total") {
		t.Errorf("missing total: %q", got)
	}
	if !strings.Contains(got, "--page 2") {
		t.Errorf("missing next page hint: %q", got)
	}
}

func TestPaginationFooterNoHint(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	defer func() { Stdout = origOut }()

	PaginationFooter(3, 3, 52, false)
	got := buf.String()
	if strings.Contains(got, "--page") {
		t.Errorf("should not show hint on last page: %q", got)
	}
}

func TestPaginationFooterQuiet(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	origQuiet := QuietMode
	QuietMode = true
	defer func() { Stdout = origOut; QuietMode = origQuiet }()

	PaginationFooter(1, 3, 52, true)
	if buf.Len() > 0 {
		t.Error("PaginationFooter should produce no output in quiet mode")
	}
}

func TestSavingsPercentStyle(t *testing.T) {
	base := lipgloss.NewStyle()

	high := savingsPercentStyle("71.8%", base)
	if high.GetForeground() != lipgloss.Color("114") {
		t.Error(">=50% should be green")
	}

	mid := savingsPercentStyle("30.0%", base)
	if mid.GetForeground() != lipgloss.Color("173") {
		t.Error("20-49% should be orange")
	}

	low := savingsPercentStyle("10.0%", base)
	if low.GetForeground() == lipgloss.Color("114") || low.GetForeground() == lipgloss.Color("173") {
		t.Error("<20% should not be colored")
	}

	invalid := savingsPercentStyle("abc", base)
	_ = invalid
}

func TestGetTermWidthFallback(t *testing.T) {
	origOut := Stdout
	Stdout = &bytes.Buffer{}
	origWidth := getTermWidth
	getTermWidth = func() int { return 80 }
	defer func() { Stdout = origOut; getTermWidth = origWidth }()

	w := getTermWidth()
	if w != 80 {
		t.Errorf("getTermWidth fallback = %d, want 80", w)
	}
}

func TestGetTermWidthCustom(t *testing.T) {
	origWidth := getTermWidth
	getTermWidth = func() int { return 200 }
	defer func() { getTermWidth = origWidth }()

	w := getTermWidth()
	if w != 200 {
		t.Errorf("getTermWidth custom = %d, want 200", w)
	}
}

func TestGetTermWidthRealClosure(t *testing.T) {
	origOut := Stdout
	origGS := termGetSize
	defer func() { Stdout = origOut; termGetSize = origGS }()

	Stdout = &bytes.Buffer{}
	if w := getTermWidth(); w != 80 {
		t.Errorf("getTermWidth with non-File Stdout = %d, want 80", w)
	}

	Stdout = os.Stdout
	termGetSize = func(int) (int, int, error) { return 200, 50, nil }
	if w := getTermWidth(); w != 200 {
		t.Errorf("getTermWidth with mocked term.GetSize success = %d, want 200", w)
	}

	termGetSize = func(int) (int, int, error) { return 0, 0, fmt.Errorf("no tty") }
	if w := getTermWidth(); w != 80 {
		t.Errorf("getTermWidth with mocked term.GetSize error = %d, want 80", w)
	}
}

func TestTableSavingsPercentColor(t *testing.T) {
	var buf bytes.Buffer
	origOut := Stdout
	Stdout = &buf
	defer func() { Stdout = origOut }()

	Table(
		[]string{"ID", "Savings %"},
		[][]string{{"rec-001", "71.8%"}, {"rec-002", "15.0%"}},
	)
	if buf.Len() == 0 {
		t.Error("table should produce output")
	}
}
