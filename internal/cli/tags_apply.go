package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsApplyCmd = &cobra.Command{
	Use:   "apply -f FILE",
	Short: "Create or replace a virtual tag from a YAML file",
	Long: `Create or replace a virtual tag from a YAML file.

The name in the file picks the key. When a virtual key of that name exists,
apply replaces its definition; otherwise it creates one. Either way it first
prints what changes, then asks before sending. A change to settings alone
(name, description, effective_from, can_override) updates only those.

--dry-run prints the changes and sends nothing. It exits 2 when there are
changes and 0 when there are none, so a CI job can catch drift.

An existing key keeps its effective_from when the file leaves it out.

A file with one config:

  name: Teams
  description: Owning team across AWS and GCP.
  configs:
    - value: data
      rules:
        - providers: [aws, gcp]
          where:
            - {dimension: service, operator: is, values: [Amazon Redshift, BigQuery]}`,
	Args: cobra.NoArgs,
	Example: `  l4 tags apply -f teams.yaml
  l4 tags apply -f teams.yaml --dry-run
  l4 tags apply -f teams.yaml --yes`,
	RunE: func(_ *cobra.Command, _ []string) error {
		return runTagsApply()
	},
}

const (
	applyCreate = "create"
	applyUpdate = "update"
	applyNone   = "none"

	markSame   = " "
	markAdd    = "+"
	markRemove = "-"
)

// Printed under --json when apply sends nothing, so a pipeline can read the planned body.
type tagApplyPlan struct {
	Action     string        `json:"action"`
	KeyID      string        `json:"key_id,omitempty"`
	Definition tagDefinition `json:"definition"`
}

type tagReplaceRequest struct {
	tagDefinition
	ExpectedVersion int `json:"expected_version"`
}

type diffEntry struct {
	mark  string
	lines []string
}

type tagDiff struct {
	settings  []string
	collapsed []diffEntry
	configs   []diffEntry
}

func (d tagDiff) rulesChanged() bool {
	return entriesChanged(d.collapsed) || entriesChanged(d.configs)
}

func (d tagDiff) changed() bool {
	return len(d.settings) > 0 || d.rulesChanged()
}

func runTagsApply() error {
	desired, err := loadTagDefinition(flagTagsFile)
	if err != nil {
		return err
	}
	if strings.TrimSpace(desired.Name) == "" {
		return fmt.Errorf("%s: name is required, it picks the virtual tag to create or replace", flagTagsFile)
	}
	current, names, err := findCurrentTag(desired.Name)
	if err != nil {
		return err
	}
	var before *tagDefinition
	if current != nil {
		before = &current.tagDefinition
		if desired.EffectiveFrom == nil {
			desired.EffectiveFrom = current.EffectiveFrom
		}
	}
	diff := diffTagDefinitions(before, desired, names)
	plan := newApplyPlan(current, desired, diff)
	if plan.Action == applyNone {
		output.Info(fmt.Sprintf("No changes: virtual tag %s already matches %s.", desired.Name, flagTagsFile))
		return output.PrintResult(plan)
	}
	printTagDiff(plan, diff)
	if flagTagsDryRun {
		output.Info("Dry run: nothing was sent.")
		if err := output.PrintResult(plan); err != nil {
			return err
		}
		return ErrIssuesFound
	}
	approved, err := requireApproval(
		fmt.Sprintf("Apply these changes to virtual tag %s?", desired.Name),
		"applying "+desired.Name,
	)
	if err != nil {
		return err
	}
	if !approved {
		output.Info("Aborted.")
		return nil
	}
	envelope, err := sendTagApply(current, desired, diff)
	if err != nil {
		return err
	}
	return renderTagApplyResult(envelope, plan)
}

func findCurrentTag(name string) (*tagKeyDetail, keyNames, error) {
	_, rows, err := listTagKeys(url.Values{paramOrigin: {tagOriginVirtual}})
	if err != nil {
		return nil, nil, err
	}
	names := newKeyNames(rows)
	row, ok := matchTagKey(rows, name)
	if !ok {
		return nil, names, nil
	}
	_, detail, err := fetchTagKeyDetail(row.ID, nil)
	if err != nil {
		return nil, nil, err
	}
	return &detail, names, nil
}

func newApplyPlan(current *tagKeyDetail, desired tagDefinition, diff tagDiff) tagApplyPlan {
	plan := tagApplyPlan{Action: applyCreate, Definition: desired}
	if current == nil {
		return plan
	}
	plan.KeyID = current.ID
	plan.Action = applyUpdate
	if !diff.changed() {
		plan.Action = applyNone
	}
	return plan
}

// A settings-only change goes through PATCH and leaves the rules untouched.
func sendTagApply(current *tagKeyDetail, desired tagDefinition, diff tagDiff) (map[string]interface{}, error) {
	switch {
	case current == nil:
		return postWrite(virtualTagsPath, desired)
	case diff.rulesChanged():
		return putWrite(virtualTagPath(current.ID), tagReplaceRequest{tagDefinition: desired, ExpectedVersion: current.RulesVersion})
	}
	return patchWrite(virtualTagPath(current.ID), settingsPatch(current.tagDefinition, desired, current.RulesVersion))
}

var appliedVerb = map[string]string{applyCreate: "Created", applyUpdate: "Updated"}

func renderTagApplyResult(envelope map[string]interface{}, plan tagApplyPlan) error {
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	var result tagMutation
	if err := decodeData(envelope, &result); err != nil {
		return err
	}
	output.Success(fmt.Sprintf("%s virtual tag %s", appliedVerb[plan.Action], plan.Definition.Name))
	output.KeyValue("ID", result.Key.ID)
	if result.Job != nil {
		output.KeyValue("Job", describeJob(result.Job))
	}
	return nil
}

func diffTagDefinitions(before *tagDefinition, after tagDefinition, names keyNames) tagDiff {
	var prev tagDefinition
	if before != nil {
		prev = *before
	}
	return tagDiff{
		settings:  settingsDiff(before, after),
		collapsed: diffItems(collapsedItems(prev.CollapsedKeys, names), collapsedItems(after.CollapsedKeys, names)),
		configs:   diffItems(configItems(prev.Configs, names), configItems(after.Configs, names)),
	}
}

type setting struct {
	name       string
	shown      string
	patchValue interface{}
}

func settingsOf(d tagDefinition) []setting {
	return []setting{
		{name: "name", shown: d.Name, patchValue: d.Name},
		{name: "description", shown: strconv.Quote(d.Description), patchValue: d.Description},
		{name: "effective_from", shown: effectiveFromLabel(d.EffectiveFrom), patchValue: d.EffectiveFrom},
		{name: "can_override", shown: strconv.FormatBool(d.CanOverride), patchValue: d.CanOverride},
	}
}

func settingsDiff(before *tagDefinition, after tagDefinition) []string {
	next := settingsOf(after)
	lines := make([]string, 0, len(next))
	if before == nil {
		for _, s := range next {
			lines = append(lines, markAdd+" "+s.name+": "+s.shown)
		}
		return lines
	}
	prev := settingsOf(*before)
	for i, s := range next {
		if prev[i].shown != s.shown {
			lines = append(lines, "~ "+s.name+": "+prev[i].shown+" -> "+s.shown)
		}
	}
	return lines
}

func settingsPatch(before, after tagDefinition, version int) map[string]interface{} {
	body := map[string]interface{}{"expected_version": version}
	prev := settingsOf(before)
	for i, s := range settingsOf(after) {
		if prev[i].shown != s.shown {
			body[s.name] = s.patchValue
		}
	}
	return body
}

// Aligned on the longest common subsequence, so a config inserted at the top reads as one addition.
func diffItems(before, after [][]string) []diffEntry {
	a, b := joinItems(before), joinItems(after)
	n, m := len(a), len(b)
	common := make([][]int, n+1)
	for i := range common {
		common[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				common[i][j] = common[i+1][j+1] + 1
			} else {
				common[i][j] = max(common[i+1][j], common[i][j+1])
			}
		}
	}
	entries := make([]diffEntry, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			entries = append(entries, diffEntry{mark: markSame, lines: after[j]})
			i++
			j++
		case common[i+1][j] >= common[i][j+1]:
			entries = append(entries, diffEntry{mark: markRemove, lines: before[i]})
			i++
		default:
			entries = append(entries, diffEntry{mark: markAdd, lines: after[j]})
			j++
		}
	}
	for ; i < n; i++ {
		entries = append(entries, diffEntry{mark: markRemove, lines: before[i]})
	}
	for ; j < m; j++ {
		entries = append(entries, diffEntry{mark: markAdd, lines: after[j]})
	}
	return entries
}

func joinItems(items [][]string) []string {
	joined := make([]string, len(items))
	for i, lines := range items {
		joined[i] = strings.Join(lines, "\n")
	}
	return joined
}

func entriesChanged(entries []diffEntry) bool {
	for _, e := range entries {
		if e.mark != markSame {
			return true
		}
	}
	return false
}

func printTagDiff(plan tagApplyPlan, diff tagDiff) {
	title := "Create virtual tag " + plan.Definition.Name
	if plan.KeyID != "" {
		title = "Update virtual tag " + plan.Definition.Name + " (" + plan.KeyID + ")"
	}
	output.Info(title)
	if len(diff.settings) > 0 {
		output.Info("Settings:")
		for _, line := range diff.settings {
			output.Info("  " + line)
		}
	}
	printDiffEntries("Collapsed keys:", diff.collapsed)
	printDiffEntries("Configs:", diff.configs)
}

func printDiffEntries(title string, entries []diffEntry) {
	if !entriesChanged(entries) {
		return
	}
	output.Info(title)
	for _, e := range entries {
		for i, line := range e.lines {
			if i > 0 {
				line = "   " + line
			}
			output.Info("  " + e.mark + " " + line)
		}
	}
}

func init() {
	tagsApplyCmd.Flags().StringVarP(&flagTagsFile, "file", "f", "", "YAML file holding the virtual tag definition")
	tagsApplyCmd.Flags().BoolVar(&flagTagsDryRun, "dry-run", false, "Print the changes without sending them. Exits 2 when there are changes")
	tagsApplyCmd.Flags().BoolVarP(&flagTagsYes, wordYes, "y", false, "Skip the confirmation prompt")
	tagsCmd.AddCommand(tagsApplyCmd)
}
