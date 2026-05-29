package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestSectionTitleMuted(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = false

	origIsTTY := output.IsTTY
	output.IsTTY = func() bool { return true }
	t.Cleanup(func() { output.IsTTY = origIsTTY })

	got := sectionTitle("USAGE")
	if !strings.Contains(got, "USAGE") {
		t.Errorf("expected USAGE in output, got %q", got)
	}
	if strings.Contains(got, "\033[1m") {
		t.Errorf("expected muted color, not bold ANSI, got %q", got)
	}
	if got == "USAGE" {
		t.Errorf("expected styled output for TTY, got plain %q", got)
	}
}

func TestSectionTitleNoColor(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = true

	got := sectionTitle("USAGE")
	if got != "USAGE" {
		t.Errorf("expected plain USAGE, got %q", got)
	}
}

func TestSectionTitleNonTTY(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = false

	origStdout := output.Stdout
	output.Stdout = &bytes.Buffer{}
	t.Cleanup(func() { output.Stdout = origStdout })

	got := sectionTitle("FLAGS")
	if got != "FLAGS" {
		t.Errorf("expected plain FLAGS for non-TTY, got %q", got)
	}
}

func TestPrintGroupHelp(t *testing.T) {
	parent := &cobra.Command{
		Use:   "test-group",
		Short: "A test group command",
	}
	child := &cobra.Command{
		Use:   "sub",
		Short: "A subcommand",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	parent.AddCommand(child)

	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	var buf bytes.Buffer
	printGroupHelp(&buf, parent)
	got := buf.String()

	for _, want := range []string{"Usage:", "Available Commands:", "Learn more:", "sub:"} {
		if !strings.Contains(got, want) {
			t.Errorf("group help missing %q:\n%s", want, got)
		}
	}
}

func TestPrintGroupHelpInheritedFlags(t *testing.T) {
	parent := &cobra.Command{
		Use:   "root",
		Short: "Root",
	}
	parent.PersistentFlags().Bool("verbose", false, "Verbose output")

	group := &cobra.Command{
		Use:   "grp",
		Short: "Group",
	}
	parent.AddCommand(group)

	child := &cobra.Command{
		Use:   "leaf",
		Short: "Leaf command",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	group.AddCommand(child)

	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	var buf bytes.Buffer
	printGroupHelp(&buf, group)
	got := buf.String()

	if !strings.Contains(got, "Inherited Flags:") {
		t.Errorf("group help missing Inherited Flags:\n%s", got)
	}
	if !strings.Contains(got, "--verbose") {
		t.Errorf("group help missing --verbose flag:\n%s", got)
	}
}

func TestPrintCommandHelp(t *testing.T) {
	cmd := &cobra.Command{
		Use:     "leaf",
		Short:   "A leaf command",
		Long:    "Detailed description of the leaf command",
		Example: "  $ l4 leaf --flag value",
		RunE:    func(cmd *cobra.Command, args []string) error { return nil },
	}
	cmd.Flags().String("flag", "", "A test flag")

	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	var buf bytes.Buffer
	printCommandHelp(&buf, cmd)
	got := buf.String()

	for _, want := range []string{"Usage:", "Flags:", "Examples:", "Learn more:", "Detailed description", "--flag"} {
		if !strings.Contains(got, want) {
			t.Errorf("command help missing %q:\n%s", want, got)
		}
	}
}

func TestPrintCommandHelpWithParent(t *testing.T) {
	parent := &cobra.Command{
		Use:   "parent",
		Short: "Parent group",
	}
	child := &cobra.Command{
		Use:   "child",
		Short: "Child command",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	parent.AddCommand(child)

	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	var buf bytes.Buffer
	printCommandHelp(&buf, child)
	got := buf.String()

	if !strings.Contains(got, "parent <command>") {
		t.Errorf("expected parent command path in LEARN MORE:\n%s", got)
	}
}

func TestPrintCommandHelpInheritedFlags(t *testing.T) {
	parent := &cobra.Command{
		Use:   "root",
		Short: "Root",
	}
	parent.PersistentFlags().Bool("global", false, "Global flag")

	child := &cobra.Command{
		Use:   "cmd",
		Short: "A command",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	parent.AddCommand(child)

	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	var buf bytes.Buffer
	printCommandHelp(&buf, child)
	got := buf.String()

	if !strings.Contains(got, "Inherited Flags:") {
		t.Errorf("command help missing Inherited Flags:\n%s", got)
	}
}

func TestHelpDescriptionLong(t *testing.T) {
	cmd := &cobra.Command{
		Short: "short desc",
		Long:  "long desc",
	}
	if got := helpDescription(cmd); got != "long desc" {
		t.Errorf("helpDescription = %q, want %q", got, "long desc")
	}
}

func TestHelpDescriptionFallback(t *testing.T) {
	cmd := &cobra.Command{
		Short: "short desc",
	}
	if got := helpDescription(cmd); got != "short desc" {
		t.Errorf("helpDescription = %q, want %q", got, "short desc")
	}
}

func TestAvailableSubcommands(t *testing.T) {
	noop := func(cmd *cobra.Command, args []string) error { return nil }
	parent := &cobra.Command{Use: "p"}
	visible := &cobra.Command{Use: "visible", Short: "Visible", RunE: noop}
	hidden := &cobra.Command{Use: "hidden", Short: "Hidden", Hidden: true, RunE: noop}
	helpCmd := &cobra.Command{Use: helpCmdName, Short: "Help", RunE: noop}
	parent.AddCommand(visible, hidden, helpCmd)

	cmds := availableSubcommands(parent)

	var names []string
	for _, c := range cmds {
		names = append(names, c.Name())
	}
	if len(cmds) != 1 || cmds[0].Name() != "visible" {
		t.Errorf("expected [visible], got %v", names)
	}
}

func TestRenderFlagsEmpty(t *testing.T) {
	fs := pflag.NewFlagSet("empty", pflag.ContinueOnError)
	if got := renderFlags(fs); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestRenderFlagsSkipsHelp(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.BoolP("help", "h", false, "help for this command")
	fs.String("name", "", "A name flag")

	got := renderFlags(fs)
	if strings.Contains(got, "--help") {
		t.Errorf("renderFlags should skip --help:\n%s", got)
	}
	if !strings.Contains(got, "--name") {
		t.Errorf("renderFlags missing --name:\n%s", got)
	}
}

func TestRenderFlagsHelpOnly(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.BoolP("help", "h", false, "help for this command")

	if got := renderFlags(fs); got != "" {
		t.Errorf("expected empty string for help-only flags, got %q", got)
	}
}

func TestL4HelpFuncGroupDispatch(t *testing.T) {
	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	group := &cobra.Command{
		Use:   "mygroup",
		Short: "A group",
	}
	child := &cobra.Command{
		Use:   "sub",
		Short: "Sub command",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}
	group.AddCommand(child)

	var buf bytes.Buffer
	group.SetOut(&buf)
	l4HelpFunc(group, nil)
	got := buf.String()

	if !strings.Contains(got, "Available Commands:") {
		t.Errorf("expected group help dispatch:\n%s", got)
	}
}

func TestL4HelpFuncLeafDispatch(t *testing.T) {
	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	leaf := &cobra.Command{
		Use:   "leaf",
		Short: "A leaf",
		RunE:  func(cmd *cobra.Command, args []string) error { return nil },
	}

	var buf bytes.Buffer
	leaf.SetOut(&buf)
	l4HelpFunc(leaf, nil)
	got := buf.String()

	if !strings.Contains(got, "A leaf") {
		t.Errorf("expected leaf help dispatch:\n%s", got)
	}
}

func TestUngroupedCommands(t *testing.T) {
	noop := func(cmd *cobra.Command, args []string) error { return nil }
	parent := &cobra.Command{Use: "p"}
	grouped := &cobra.Command{Use: "grouped", Short: "Grouped", GroupID: "g", RunE: noop}
	ungroupedCmd := &cobra.Command{Use: "extra", Short: "Extra", RunE: noop}
	parent.AddGroup(&cobra.Group{ID: "g", Title: "Group:"})
	parent.AddCommand(grouped, ungroupedCmd)

	cmds := ungroupedCommands(parent)
	if len(cmds) != 1 || cmds[0].Name() != "extra" {
		var names []string
		for _, c := range cmds {
			names = append(names, c.Name())
		}
		t.Errorf("expected [extra], got %v", names)
	}
}

func TestPrintRootHelpUngrouped(t *testing.T) {
	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	root := &cobra.Command{
		Use:     "test",
		Short:   "Test CLI",
		Example: "  $ test foo",
	}
	root.PersistentFlags().Bool("verbose", false, "Verbose")
	root.AddGroup(&cobra.Group{ID: "main", Title: "Main:"})

	grouped := &cobra.Command{Use: "foo", Short: "Foo cmd", GroupID: "main", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	extra := &cobra.Command{Use: "extra", Short: "Extra cmd", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	root.AddCommand(grouped, extra)

	var buf bytes.Buffer
	printRootHelp(&buf, root)
	got := buf.String()

	if !strings.Contains(got, "Additional Commands:") {
		t.Errorf("root help missing Additional Commands:\n%s", got)
	}
	if !strings.Contains(got, "extra:") {
		t.Errorf("root help missing extra command:\n%s", got)
	}
}

func TestRenderFlagsSkipsBlankLines(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String("aaa", "", "First flag\n\nWith blank line in description")

	got := renderFlags(fs)
	if !strings.Contains(got, "--aaa") {
		t.Errorf("renderFlags missing --aaa:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			t.Errorf("renderFlags should skip blank lines, got blank in:\n%s", got)
		}
	}
}

func TestPrintRootHelpNoFlags(t *testing.T) {
	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	root := &cobra.Command{
		Use:   "bare",
		Short: "Bare CLI",
	}
	root.AddGroup(&cobra.Group{ID: "main", Title: "Main:"})
	child := &cobra.Command{Use: "sub", Short: "Sub", GroupID: "main", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	root.AddCommand(child)

	var buf bytes.Buffer
	printRootHelp(&buf, root)
	got := buf.String()

	if strings.Contains(got, "Flags:") {
		t.Errorf("root help should omit Flags when no persistent flags:\n%s", got)
	}
}

func TestPrintRootHelpEmptyGroup(t *testing.T) {
	origNoColor := output.NoColor
	output.NoColor = true
	t.Cleanup(func() { output.NoColor = origNoColor })

	root := &cobra.Command{
		Use:   "test",
		Short: "Test CLI",
	}
	root.AddGroup(
		&cobra.Group{ID: "populated", Title: "Populated:"},
		&cobra.Group{ID: "empty", Title: "Empty:"},
	)
	child := &cobra.Command{Use: "cmd", Short: "Cmd", GroupID: "populated", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	root.AddCommand(child)

	var buf bytes.Buffer
	printRootHelp(&buf, root)
	got := buf.String()

	if strings.Contains(got, "Empty:") {
		t.Errorf("root help should skip empty groups:\n%s", got)
	}
	if !strings.Contains(got, "Populated:") {
		t.Errorf("root help missing populated group:\n%s", got)
	}
}

func TestPrintRootHelpFlags(t *testing.T) {
	outBuf, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Flags:") {
		t.Errorf("root help missing Flags section:\n%s", got)
	}
	if !strings.Contains(got, "--json") {
		t.Errorf("root help missing --json flag:\n%s", got)
	}
}

func TestPrintRootHelpSections(t *testing.T) {
	outBuf, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help error: %v", err)
	}
	got := outBuf.String()

	for _, section := range []string{
		"Usage:",
		"Core Commands:",
		"Authentication:",
		"Configuration:",
		"Flags:",
		"Environment:",
		"Examples:",
		"Learn more:",
	} {
		if !strings.Contains(got, section) {
			t.Errorf("root help missing %q section", section)
		}
	}
}

func TestColorizeExamples(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = false

	origIsTTY := output.IsTTY
	output.IsTTY = func() bool { return true }
	t.Cleanup(func() { output.IsTTY = origIsTTY })

	input := "  $ l4 estimate ./infra/\n  $ l4 diff baseline.json"
	got := colorizeExamples(input)

	// The styled output should differ from the plain input
	if got == input {
		t.Errorf("expected colorized output, got unchanged: %q", got)
	}
	// Each line should contain ANSI escape sequences
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.Contains(line, "\033[") {
			t.Errorf("expected ANSI escapes in line, got %q", line)
		}
	}
}

func TestColorizeExamplesNoColor(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = true

	input := "  $ l4 estimate ./infra/"
	got := colorizeExamples(input)
	if got != input {
		t.Errorf("expected plain text with NoColor, got %q", got)
	}
}

func TestColorizeExamplesNonTTY(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = false

	origStdout := output.Stdout
	output.Stdout = &bytes.Buffer{}
	t.Cleanup(func() { output.Stdout = origStdout })

	input := "  $ l4 estimate ./infra/"
	got := colorizeExamples(input)
	if got != input {
		t.Errorf("expected plain text for non-TTY, got %q", got)
	}
}

func TestColorizeExamplesNonCommandLines(t *testing.T) {
	origNoColor := output.NoColor
	t.Cleanup(func() { output.NoColor = origNoColor })
	output.NoColor = false

	origIsTTY := output.IsTTY
	output.IsTTY = func() bool { return true }
	t.Cleanup(func() { output.IsTTY = origIsTTY })

	input := "  Run an estimate:\n  $ l4 estimate ./infra/"
	got := colorizeExamples(input)
	lines := strings.Split(got, "\n")

	// Description line should remain plain
	if lines[0] != "  Run an estimate:" {
		t.Errorf("expected description line unchanged, got %q", lines[0])
	}
	// Command line should be styled (different from input)
	if lines[1] == "  $ l4 estimate ./infra/" {
		t.Errorf("expected command line to be colorized, got plain: %q", lines[1])
	}
}
