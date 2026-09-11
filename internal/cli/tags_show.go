package cli

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

var tagsShowCmd = &cobra.Command{
	Use:   "show <key>",
	Short: "Show one tag key: its rules, spend per value and unallocated spend",
	Long: `Show one tag key: its collapsed keys and configs in the order they are
checked, spend per value, unallocated spend, status and the latest
reprocessing job.

Each config reads as a sentence, value first:

  1. data <- aws, gcp where service is Amazon Redshift, BigQuery`,
	Args: cobra.ExactArgs(1),
	Example: `  l4 tags show Teams
  l4 tags show vtk_0123abcd --start 2026-08-01 --end 2026-08-31`,
	RunE: func(_ *cobra.Command, args []string) error {
		return runTagsShow(args[0])
	},
}

func runTagsShow(ref string) error {
	if err := validateWindow(); err != nil {
		return err
	}
	id, rows, err := resolveTagKey(ref, "")
	if err != nil {
		return err
	}
	envelope, detail, err := fetchTagKeyDetail(id, windowParams())
	if err != nil {
		return err
	}
	if output.HasFormattingFlags() {
		return output.PrintResult(envelope)
	}
	names, err := namesForDetail(detail, rows)
	if err != nil {
		return err
	}
	renderTagKeyDetail(detail, names)
	return nil
}

// Read only when a stored key id would otherwise reach the reader.
func namesForDetail(detail tagKeyDetail, rows []tagKeyRow) (keyNames, error) {
	if rows == nil && detailNamesKeys(detail) {
		var err error
		if _, rows, err = listTagKeys(url.Values{}); err != nil {
			return nil, err
		}
	}
	return newKeyNames(rows), nil
}

func detailNamesKeys(detail tagKeyDetail) bool {
	for _, k := range detail.CollapsedKeys {
		if k.Source.Origin == tagOriginVirtual {
			return true
		}
	}
	for _, c := range detail.Configs {
		virtualSource := c.Assign.Source != nil && c.Assign.Source.Origin == tagOriginVirtual
		input := c.Assign.InputFilter != nil && filterNamesKey(*c.Assign.InputFilter)
		if virtualSource || input || filterNamesKey(c.Filter) {
			return true
		}
	}
	return false
}

func filterNamesKey(f tagFilter) bool {
	for _, r := range f.Rules {
		for _, cond := range r.Conditions {
			if cond.Dimension == dimensionVirtualTag {
				return true
			}
		}
	}
	return false
}

func renderTagKeyDetail(d tagKeyDetail, names keyNames) {
	output.Header(d.Name)
	output.KeyValue("ID", d.ID)
	output.KeyValue("Origin", d.Origin)
	output.KeyValue("Description", orDash(d.Description))
	output.KeyValue("Providers", orDash(strings.Join(d.Providers, ", ")))
	output.KeyValue("Status", output.StatusBadge(orDash(d.Status)))
	if d.Origin == tagOriginVirtual {
		output.KeyValue("Effective from", effectiveFromLabel(d.EffectiveFrom))
		output.KeyValue("Can override", strconv.FormatBool(d.CanOverride))
		output.KeyValue("Version", strconv.Itoa(d.RulesVersion))
	}
	output.KeyValue("Window", windowLabel(d.Window))
	if d.Job != nil {
		output.KeyValue("Job", describeJob(d.Job))
	}
	if len(d.Dependents) > 0 {
		output.KeyValue("Read by", refNames(d.Dependents))
	}
	output.Info("")
	output.KPICards([]output.KPICard{
		{Label: "Total spend", Value: formatSpend(d.TotalSpend)},
		{Label: "Unallocated", Value: formatSpend(d.UnallocatedSpend)},
		{Label: "Resources", Value: strconv.Itoa(d.ResourceCount)},
	})
	printNumbered("Collapsed keys, checked before the configs:", collapsedItems(d.CollapsedKeys, names))
	printNumbered("Configs, first match wins:", configItems(d.Configs, names))
	renderTagValues(d.Values)
}

func renderTagValues(values []tagValueSummary) {
	if len(values) == 0 {
		output.Info("No value holds spend in this window.")
		return
	}
	rows := make([][]string, 0, len(values))
	for _, v := range values {
		rows = append(rows, []string{v.Name, formatSpend(v.Spend), strconv.Itoa(v.ResourceCount)})
	}
	output.Table([]string{"Value", "Spend", "Resources"}, rows)
}

func printNumbered(title string, items [][]string) {
	if len(items) == 0 {
		return
	}
	output.Header(title)
	for i, lines := range items {
		marker := strconv.Itoa(i+1) + "."
		pad := strings.Repeat(" ", len(marker))
		for j, line := range lines {
			lead := pad
			if j == 0 {
				lead = marker
			}
			output.Info("  " + lead + " " + line)
		}
	}
	output.Info("")
}

// A rule pointing at another virtual key reads the same whether the API stored its id or its name.
type keyNames map[string]string

func newKeyNames(rows []tagKeyRow) keyNames {
	names := keyNames{}
	for _, r := range rows {
		names[strings.ToLower(r.ID)] = r.Name
		names[strings.ToLower(r.Name)] = r.Name
	}
	return names
}

func (n keyNames) of(ref string) string {
	if name, ok := n[strings.ToLower(ref)]; ok {
		return name
	}
	return ref
}

func collapsedItems(keys []tagCollapsedKey, names keyNames) [][]string {
	items := make([][]string, 0, len(keys))
	for _, k := range keys {
		items = append(items, []string{collapsedText(k, names)})
	}
	return items
}

func collapsedText(k tagCollapsedKey, names keyNames) string {
	scope := "every provider"
	if k.Scope != nil && len(k.Scope.Providers) > 0 {
		scope = providersText(k.Scope.Providers)
	}
	text := sourceText(&k.Source, names) + " on " + scope
	if k.ValuePrefix != "" {
		text += " with prefix " + strconv.Quote(k.ValuePrefix)
	}
	return text
}

func sourceText(s *tagSource, names keyNames) string {
	if s == nil {
		return "no source"
	}
	if s.Origin == tagOriginVirtual {
		return "virtual key " + names.of(s.Key)
	}
	return s.Origin + " key " + s.Key
}

func configItems(configs []tagConfig, names keyNames) [][]string {
	items := make([][]string, 0, len(configs))
	for _, c := range configs {
		items = append(items, configLines(c, names))
	}
	return items
}

func configLines(c tagConfig, names keyNames) []string {
	rules := c.Filter.Rules
	first := rulesText(rules[:min(1, len(rules))], names)
	lines := []string{assignText(c.Assign, names) + " <- " + first + timeFrameText(c.TimeFrame)}
	for i := 1; i < len(rules); i++ {
		lines = append(lines, "or "+ruleText(rules[i], names))
	}
	if c.Assign.InputFilter != nil {
		lines = append(lines, "measured on "+rulesText(c.Assign.InputFilter.Rules, names))
	}
	return lines
}

func assignText(a tagAssign, names keyNames) string {
	switch a.Type {
	case assignValue:
		return valueText(derefString(a.Value))
	case assignPercent:
		parts := make([]string, 0, len(a.Splits))
		for _, s := range a.Splits {
			parts = append(parts, valueText(s.Value)+" "+strconv.FormatFloat(s.Pct, 'f', -1, 64)+"%")
		}
		return strings.Join(parts, ", ")
	case assignCostBased:
		return "split by " + sourceText(a.Source, names)
	}
	return a.Type
}

func rulesText(rules []tagRule, names keyNames) string {
	if len(rules) == 0 {
		return "no rules"
	}
	parts := make([]string, len(rules))
	for i, r := range rules {
		parts[i] = ruleText(r, names)
	}
	return strings.Join(parts, " or ")
}

func ruleText(r tagRule, names keyNames) string {
	providers := providersText(r.Providers)
	if len(r.Conditions) == 0 {
		return providers
	}
	conditions := make([]string, len(r.Conditions))
	for i, c := range r.Conditions {
		conditions[i] = conditionText(c, names)
	}
	return providers + " where " + strings.Join(conditions, " and ")
}

// Sorted because providers are a set and the API may not keep the file's order.
func providersText(providers []string) string {
	if len(providers) == 0 {
		return "no provider"
	}
	sorted := slices.Clone(providers)
	slices.Sort(sorted)
	return strings.Join(sorted, ", ")
}

func conditionText(c tagCondition, names keyNames) string {
	if c.Dimension == dimensionTagged {
		return taggedText(c)
	}
	subject := strings.ReplaceAll(c.Dimension, "_", " ")
	if c.Key != "" {
		key := c.Key
		if c.Dimension == dimensionVirtualTag {
			key = names.of(c.Key)
		}
		subject += " " + key
	}
	return subject + " " + operatorText(c.Operator) + " " + valuesText(c.Values)
}

func taggedText(c tagCondition) string {
	switch {
	case c.Operator == opIsNot && c.Key == "":
		return "has no tag"
	case c.Operator == opIsNot:
		return "lacks tag " + c.Key
	case c.Key == "":
		return "has any tag"
	}
	return "has tag " + c.Key
}

var operatorWords = map[string]string{
	"":                   opIs,
	opIsNot:              "is not",
	"not_contains":       "does not contain",
	"starts_with":        "starts with",
	"ends_with":          "ends with",
	"flexible_match":     "flexibly matches",
	"not_flexible_match": "does not flexibly match",
}

func operatorText(op string) string {
	if word, ok := operatorWords[op]; ok {
		return word
	}
	return op
}

func valuesText(values []string) string {
	if len(values) == 0 {
		return "(no values)"
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = valueText(v)
	}
	return strings.Join(parts, ", ")
}

// Quoted when a comma-separated list of values would otherwise read ambiguously.
func valueText(v string) string {
	if v == "" || strings.Contains(v, ",") || strings.TrimSpace(v) != v {
		return strconv.Quote(v)
	}
	return v
}

func timeFrameText(tf *tagTimeFrame) string {
	if tf == nil {
		return ""
	}
	switch {
	case tf.Start != "" && tf.End != "":
		return fmt.Sprintf(" (%s to %s)", tf.Start, tf.End)
	case tf.Start != "":
		return " (from " + tf.Start + ")"
	case tf.End != "":
		return " (until " + tf.End + ")"
	}
	return ""
}

const currentMonth = "current month"

func effectiveFromLabel(month *string) string {
	if v := derefString(month); v != "" {
		return v
	}
	return currentMonth
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func init() {
	addWindowFlags(tagsShowCmd)
	tagsCmd.AddCommand(tagsShowCmd)
}
