package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete a virtual tag",
	Long: `Delete a virtual tag and the values it assigns.

Only virtual keys can be deleted, since provider tags come from the bill. The
API refuses to delete a key another virtual key reads, and names the keys that
read it.`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 tags delete Teams
  l4 tags delete vtk_0123abcd --yes`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runTagsDelete(args[0])
	},
}

func runTagsDelete(ref string) error {
	if strings.HasPrefix(ref, providerKeyPrefix) {
		return fmt.Errorf("%s is a provider tag key: provider tags come from the bill and cannot be deleted", ref)
	}
	// Before the lookup: an unattended run should not spend a request to fail on a missing flag.
	approved, err := requireApproval(fmt.Sprintf("Delete virtual tag %s?", ref), "deleting "+ref)
	if err != nil {
		return err
	}
	if !approved {
		output.Info("Aborted.")
		return nil
	}
	id, err := resolveTagKeyID(ref, tagOriginVirtual)
	if err != nil {
		return err
	}
	if err := deleteWrite(virtualTagPath(id)); err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(map[string]interface{}{"id": id, "deleted": true})
	}
	output.Success(fmt.Sprintf("Deleted virtual tag %s", ref))
	return nil
}

func init() {
	tagsDeleteCmd.Flags().BoolVarP(&flagTagsYes, wordYes, "y", false, "Skip the confirmation prompt")
	tagsCmd.AddCommand(tagsDeleteCmd)
}
