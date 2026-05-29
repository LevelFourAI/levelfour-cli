package cli

import (
	"github.com/spf13/cobra"
)

const shellBash = "bash"

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell autocompletion script for the specified shell.

To load completions:

Bash:
  $ source <(l4 completion bash)
  $ l4 completion bash > /etc/bash_completion.d/l4

Zsh:
  $ source <(l4 completion zsh)
  $ l4 completion zsh > "${fpath[1]}/_l4"

Fish:
  $ l4 completion fish | source
  $ l4 completion fish > ~/.config/fish/completions/l4.fish

PowerShell:
  PS> l4 completion powershell | Out-String | Invoke-Expression
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{shellBash, "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		switch args[0] {
		case shellBash:
			return rootCmd.GenBashCompletion(out)
		case "zsh":
			return rootCmd.GenZshCompletion(out)
		case "fish":
			return rootCmd.GenFishCompletion(out, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(out)
		}
		return nil
	},
}
