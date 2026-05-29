package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const helpCmdName = "help"

const (
	ansiMuted  = "\033[38;5;242m"
	ansiAccent = "\033[38;5;159m"
	ansiReset  = "\033[0m"
)

func sectionTitle(s string) string {
	if output.NoColor || !output.IsTTY() {
		return s
	}
	return ansiMuted + s + ansiReset
}

func initHelp() {
	rootCmd.SetHelpFunc(l4HelpFunc)
}

func l4HelpFunc(cmd *cobra.Command, _ []string) {
	out := cmd.OutOrStdout()
	switch {
	case cmd == rootCmd:
		printRootHelp(out, cmd)
	case cmd.HasAvailableSubCommands() && !cmd.Runnable():
		printGroupHelp(out, cmd)
	default:
		printCommandHelp(out, cmd)
	}
}

func printRootHelp(out io.Writer, cmd *cobra.Command) {
	fmt.Fprintln(out, cmd.Short)
	fmt.Fprintln(out)

	fmt.Fprintln(out, sectionTitle("Usage:"))
	fmt.Fprintf(out, "  %s <command> <subcommand> [flags]\n", cmd.Name())
	fmt.Fprintln(out)

	for _, group := range cmd.Groups() {
		cmds := commandsInGroup(cmd, group.ID)
		if len(cmds) == 0 {
			continue
		}
		fmt.Fprintln(out, sectionTitle(group.Title))
		pad := maxNameLen(cmds) + 1
		for _, c := range cmds {
			fmt.Fprintf(out, "  %-*s  %s\n", pad, c.Name()+":", c.Short)
		}
		fmt.Fprintln(out)
	}

	ungrouped := ungroupedCommands(cmd)
	if len(ungrouped) > 0 {
		fmt.Fprintln(out, sectionTitle("Additional Commands:"))
		pad := maxNameLen(ungrouped) + 1
		for _, c := range ungrouped {
			fmt.Fprintf(out, "  %-*s  %s\n", pad, c.Name()+":", c.Short)
		}
		fmt.Fprintln(out)
	}

	if f := renderFlags(cmd.PersistentFlags()); f != "" {
		fmt.Fprintln(out, sectionTitle("Flags:"))
		fmt.Fprint(out, f)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, sectionTitle("Environment:"))
	fmt.Fprintln(out, "  LEVELFOUR_TOKEN    API token (for CI pipelines, GitHub Actions secrets)")
	fmt.Fprintln(out, "  LEVELFOUR_API      API base URL override")
	fmt.Fprintln(out, "  NO_COLOR           Disable colored output")
	fmt.Fprintln(out)

	if cmd.HasExample() {
		fmt.Fprintln(out, sectionTitle("Examples:"))
		fmt.Fprintln(out)
		fmt.Fprintln(out, colorizeExamples(cmd.Example))
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, sectionTitle("Learn more:"))
	fmt.Fprintf(out, "  Use `%s <command> <subcommand> --help` for more information about a command.\n", cmd.Name())
	fmt.Fprintln(out, "  Read the docs at https://docs.levelfour.ai/cli")
}

func printGroupHelp(out io.Writer, cmd *cobra.Command) {
	fmt.Fprintln(out, helpDescription(cmd))
	fmt.Fprintln(out)

	fmt.Fprintln(out, sectionTitle("Usage:"))
	fmt.Fprintf(out, "  %s <command> [flags]\n", cmd.CommandPath())
	fmt.Fprintln(out)

	cmds := availableSubcommands(cmd)
	if len(cmds) > 0 {
		fmt.Fprintln(out, sectionTitle("Available Commands:"))
		pad := maxNameLen(cmds) + 1
		for _, c := range cmds {
			fmt.Fprintf(out, "  %-*s  %s\n", pad, c.Name()+":", c.Short)
		}
		fmt.Fprintln(out)
	}

	if f := renderFlags(cmd.InheritedFlags()); f != "" {
		fmt.Fprintln(out, sectionTitle("Inherited Flags:"))
		fmt.Fprint(out, f)
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, sectionTitle("Learn more:"))
	fmt.Fprintf(out, "  Use `%s <command> --help` for more information about a command.\n", cmd.CommandPath())
	fmt.Fprintln(out, "  Read the docs at https://docs.levelfour.ai/cli")
}

func printCommandHelp(out io.Writer, cmd *cobra.Command) {
	fmt.Fprintln(out, helpDescription(cmd))
	fmt.Fprintln(out)

	fmt.Fprintln(out, sectionTitle("Usage:"))
	fmt.Fprintf(out, "  %s\n", cmd.UseLine())
	fmt.Fprintln(out)

	if f := renderFlags(cmd.LocalFlags()); f != "" {
		fmt.Fprintln(out, sectionTitle("Flags:"))
		fmt.Fprint(out, f)
		fmt.Fprintln(out)
	}

	if f := renderFlags(cmd.InheritedFlags()); f != "" {
		fmt.Fprintln(out, sectionTitle("Inherited Flags:"))
		fmt.Fprint(out, f)
		fmt.Fprintln(out)
	}

	if cmd.HasExample() {
		fmt.Fprintln(out, sectionTitle("Examples:"))
		fmt.Fprintln(out)
		fmt.Fprintln(out, colorizeExamples(cmd.Example))
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, sectionTitle("Learn more:"))
	parent := cmd.Parent()
	if parent != nil && parent != rootCmd {
		fmt.Fprintf(out, "  Use `%s <command> --help` for more information about a command.\n", parent.CommandPath())
	} else {
		fmt.Fprintf(out, "  Use `%s <command> --help` for more information about a command.\n", rootCmd.Name())
	}
	fmt.Fprintln(out, "  Read the docs at https://docs.levelfour.ai/cli")
}

func colorizeExamples(examples string) string {
	if output.NoColor || !output.IsTTY() {
		return examples
	}
	var lines []string
	for _, line := range strings.Split(examples, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "$ ") {
			lines = append(lines, ansiAccent+line+ansiReset)
		} else {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func helpDescription(cmd *cobra.Command) string {
	if cmd.Long != "" {
		return cmd.Long
	}
	return cmd.Short
}

func commandsInGroup(parent *cobra.Command, groupID string) []*cobra.Command {
	var cmds []*cobra.Command
	for _, c := range parent.Commands() {
		if c.GroupID == groupID && c.IsAvailableCommand() && c.Name() != helpCmdName {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

func ungroupedCommands(parent *cobra.Command) []*cobra.Command {
	var cmds []*cobra.Command
	for _, c := range parent.Commands() {
		if c.GroupID == "" && c.IsAvailableCommand() && c.Name() != helpCmdName {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

func availableSubcommands(parent *cobra.Command) []*cobra.Command {
	var cmds []*cobra.Command
	for _, c := range parent.Commands() {
		if c.IsAvailableCommand() && c.Name() != helpCmdName {
			cmds = append(cmds, c)
		}
	}
	return cmds
}

func maxNameLen(cmds []*cobra.Command) int {
	n := 0
	for _, c := range cmds {
		if len(c.Name()) > n {
			n = len(c.Name())
		}
	}
	return n
}

func renderFlags(flags *pflag.FlagSet) string {
	if !flags.HasAvailableFlags() {
		return ""
	}
	usages := flags.FlagUsages()
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(usages, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "-h,") || strings.HasPrefix(trimmed, "--help ") || trimmed == "--help" {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
