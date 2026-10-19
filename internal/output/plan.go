package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/charmbracelet/lipgloss"
)

var GravitonEnabled bool

var (
	planGreen  = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	planRed    = lipgloss.NewStyle().Foreground(lipgloss.Color("167"))
	planYellow = lipgloss.NewStyle().Foreground(lipgloss.Color("173"))
	planDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	planBold   = lipgloss.NewStyle().Bold(true)
)

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefBool(p *bool) bool {
	if p == nil {
		return false
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func applyStyle(s lipgloss.Style, text string) string {
	if NoColor {
		return text
	}
	return s.Render(text)
}

func insertCommas(s string) string {
	var result []byte
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, s[i])
	}
	return string(result)
}

func formatCost(v float64) string {
	negative := v < 0
	if negative {
		v = -v
	}
	s := fmt.Sprintf("%.2f", v)
	parts := strings.SplitN(s, ".", 2)
	formatted := insertCommas(parts[0]) + "." + parts[1]
	if negative {
		return "-$" + formatted
	}
	return "$" + formatted
}

func formatDeltaCost(d float64) string {
	if d > 0 {
		return "+" + formatCost(d)
	}
	if d < 0 {
		return formatCost(d)
	}
	return formatCost(0)
}

func ptrF(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func treeConnector(i, total int) string {
	if i == total-1 {
		return "└─"
	}
	return "├─"
}

// treeIndent is how far a tree level sits inside its parent, in columns.
const treeIndent = 3

func padRight(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

func padLeft(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// The change types the API reports for a resource.
const (
	changeAdded    = "added"
	changeRemoved  = "removed"
	changeModified = "modified"
)

func symbolAndStyle(changeType string) (string, lipgloss.Style) {
	switch changeType {
	case changeAdded:
		return "+", planGreen
	case changeRemoved:
		return "-", planRed
	case changeModified:
		return "~", planYellow
	default:
		return " ", lipgloss.NewStyle()
	}
}

func formatQty(units *float64) string {
	if units == nil {
		return ""
	}
	v := *units
	if v == float64(int64(v)) {
		return formatNumber(int64(v))
	}
	return fmt.Sprintf("%.2f", v)
}

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if n < 0 {
		return "-" + insertCommas(s[1:])
	}
	return insertCommas(s)
}

func unitLabel(u *string) string {
	if u == nil {
		return ""
	}
	return *u
}

type moduleNode struct {
	path      string
	children  []*moduleNode
	resources []*api.ResourceCostEstimate
}

func splitModulePath(path string) []string {
	if path == "" {
		return nil
	}
	if !strings.HasPrefix(path, "module.") {
		return []string{path}
	}
	const sep = ".module."
	var segments []string
	start := 0
	for {
		idx := strings.Index(path[start+len("module."):], sep)
		if idx < 0 {
			segments = append(segments, path)
			break
		}
		end := start + len("module.") + idx
		segments = append(segments, path[:end])
		start = end + 1
	}
	return segments
}

// buildModuleTree groups estimates by root (when there is more than one), then
// by nested CloudFormation stack and Terraform module.
func buildModuleTree(estimates []*api.ResourceCostEstimate) *moduleNode {
	byScope := distinctScopes(estimates) > 1
	root := &moduleNode{}
	nodeMap := map[string]*moduleNode{"": root}

	for _, e := range estimates {
		key := ""
		for _, seg := range groupSegments(e, byScope) {
			childKey := key + "\x00" + seg
			if _, ok := nodeMap[childKey]; !ok {
				node := &moduleNode{path: seg}
				nodeMap[childKey] = node
				nodeMap[key].children = append(nodeMap[key].children, node)
			}
			key = childKey
		}
		nodeMap[key].resources = append(nodeMap[key].resources, e)
	}
	return root
}

func distinctScopes(estimates []*api.ResourceCostEstimate) int {
	scopes := make(map[string]bool)
	for _, e := range estimates {
		scopes[derefStr(e.Scope)] = true
	}
	return len(scopes)
}

func groupSegments(e *api.ResourceCostEstimate, byScope bool) []string {
	var segments []string
	if byScope {
		segments = scopeSegments(derefStr(e.Scope))
	}
	return append(segments, splitModulePath(derefStr(e.ModulePath))...)
}

var templateExtensions = map[string]bool{".yaml": true, ".yml": true, ".json": true, ".template": true}

// scopeSegments splits a scope into its root and, for a CloudFormation
// template, each nested stack below it: "app.yaml/Db/Cache" gives "app.yaml",
// "app.yaml/Db" and "app.yaml/Db/Cache".
func scopeSegments(scope string) []string {
	parts := strings.Split(scope, "/")
	for i, part := range parts {
		if !templateExtensions[filepathExt(part)] {
			continue
		}
		segments := []string{strings.Join(parts[:i+1], "/")}
		for j := i + 1; j < len(parts); j++ {
			segments = append(segments, strings.Join(parts[:j+1], "/"))
		}
		return segments
	}
	if scope == "" {
		return nil
	}
	return []string{scope}
}

func filepathExt(name string) string {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[i:]
	}
	return ""
}

// resourceLabel names a resource the way its language does: `aws_instance.web`
// for Terraform, `AWS::EC2::Instance Web` for CloudFormation.
func resourceLabel(resourceType, name string) string {
	if strings.Contains(resourceType, "::") {
		return resourceType + " " + name
	}
	return resourceType + "." + name
}

func renderComponentRow(w io.Writer, c *api.CostComponentEstimate, connector string, indent, colName, colQty, colUnit, colCost int) {
	qty := formatQty(c.Units)
	unit := unitLabel(c.UnitLabel)
	costStr := formatCost(c.MonthlyCost)

	nameWidth := colName - 4 - indent
	if nameWidth < 10 {
		nameWidth = 10
	}
	name := padRight(c.Name, nameWidth)
	qtyCol := padLeft(qty, colQty)
	unitCol := "  " + padRight(unit, colUnit)
	costCol := padLeft(costStr, colCost)

	prefix := strings.Repeat(" ", indent)
	fmt.Fprintf(w, " %s%s %s%s%s%s\n",
		prefix, applyStyle(planDim, connector), name, qtyCol, unitCol, costCol)
}

func visibleComponents(components []*api.CostComponentEstimate) []*api.CostComponentEstimate {
	var visible []*api.CostComponentEstimate
	for _, c := range components {
		if c.MonthlyCost != 0 || c.Units == nil {
			visible = append(visible, c)
		}
	}
	if len(visible) == 0 {
		return components
	}
	return visible
}

func isFreeResource(e *api.ResourceCostEstimate) bool {
	return e.Note != nil && *e.Note == "Free resource"
}

func isUnsupportedResource(e *api.ResourceCostEstimate) bool {
	if e.Note == nil {
		return false
	}
	return strings.HasSuffix(*e.Note, "pricing not yet supported")
}

func isZeroCostResource(e *api.ResourceCostEstimate) bool {
	if e.NewMonthlyCost != nil && *e.NewMonthlyCost != 0 {
		return false
	}
	for _, c := range e.Components {
		if c.MonthlyCost != 0 {
			return false
		}
	}
	return true
}

func groupBySubresource(components []*api.CostComponentEstimate) (topLevel []*api.CostComponentEstimate, groups []struct {
	name       string
	components []*api.CostComponentEstimate
}) {
	order := []string{}
	grouped := map[string][]*api.CostComponentEstimate{}
	for _, c := range components {
		if c.Subresource == nil {
			topLevel = append(topLevel, c)
		} else {
			key := *c.Subresource
			if _, ok := grouped[key]; !ok {
				order = append(order, key)
			}
			grouped[key] = append(grouped[key], c)
		}
	}
	for _, name := range order {
		groups = append(groups, struct {
			name       string
			components []*api.CostComponentEstimate
		}{name: name, components: grouped[name]})
	}
	return
}

func renderBreakdownResource(w io.Writer, e *api.ResourceCostEstimate, indent, colName, colQty, colUnit, colCost int) {
	components := visibleComponents(e.Components)
	topLevel, subGroups := groupBySubresource(components)
	total := len(topLevel) + len(subGroups)
	idx := 0
	for _, c := range topLevel {
		renderComponentRow(w, c, treeConnector(idx, total), indent, colName, colQty, colUnit, colCost)
		idx++
	}
	for _, g := range subGroups {
		conn := treeConnector(idx, total)
		prefix := strings.Repeat(" ", indent)
		fmt.Fprintf(w, " %s%s %s\n", prefix, applyStyle(planDim, conn), applyStyle(planBold, g.name))
		subIndent := indent + treeIndent + 1
		for j, c := range g.components {
			renderComponentRow(w, c, treeConnector(j, len(g.components)), subIndent, colName, colQty, colUnit, colCost)
		}
		idx++
	}
}

func renderBreakdownNode(w io.Writer, node *moduleNode, depth int, parentConn string, colName, colQty, colUnit, colCost int) {
	if node.path != "" {
		indent := strings.Repeat("   ", depth-1)
		fmt.Fprintf(w, "\n %s%s %s\n", indent, applyStyle(planDim, parentConn), applyStyle(planBold, node.path))
	}

	var visibleResources []*api.ResourceCostEstimate
	for _, e := range node.resources {
		if isFreeResource(e) || isUnsupportedResource(e) {
			continue
		}
		if depth > 0 && isZeroCostResource(e) {
			continue
		}
		visibleResources = append(visibleResources, e)
	}

	totalItems := len(visibleResources) + len(node.children)
	itemIdx := 0

	for _, e := range visibleResources {
		conn := treeConnector(itemIdx, totalItems)
		if depth == 0 {
			fmt.Fprintf(w, "\n %s\n", applyStyle(planBold, resourceLabel(e.ResourceType, e.ResourceName)))
			renderBreakdownResource(w, e, 0, colName, colQty, colUnit, colCost)
		} else {
			resourceIndent := strings.Repeat("   ", depth)
			fmt.Fprintf(w, " %s%s %s\n", resourceIndent, applyStyle(planDim, conn), applyStyle(planBold, resourceLabel(e.ResourceType, e.ResourceName)))
			compIndent := len(resourceIndent) + treeIndent + 1
			renderBreakdownResource(w, e, compIndent, colName, colQty, colUnit, colCost)
		}
		itemIdx++
	}

	for _, child := range node.children {
		childConn := treeConnector(itemIdx, totalItems)
		renderBreakdownNode(w, child, depth+1, childConn, colName, colQty, colUnit, colCost)
		itemIdx++
	}
}

func hasEstimatedComponents(data *api.AnalyzePrResponse) bool {
	for _, e := range data.ResourceCostEstimates {
		for _, c := range e.Components {
			if derefBool(c.IsEstimated) {
				return true
			}
		}
	}
	return false
}

func RenderBreakdown(w io.Writer, data *api.AnalyzePrResponse, projectLabel string) {
	if QuietMode {
		return
	}
	data, projectLabel = sanitizedResponse(data), Sanitize(projectLabel)

	colName := 55
	colQty := 10
	colUnit := 14
	colCost := 14

	fmt.Fprintf(w, "\n%s %s\n", applyStyle(planBold, "Project:"), projectLabel)
	fmt.Fprintf(w, "\n %s%s%s%s\n",
		applyStyle(planDim, padRight("Name", colName)),
		applyStyle(planDim, padLeft("Quantity", colQty)),
		applyStyle(planDim, "  "+padRight("Unit", colUnit)),
		applyStyle(planDim, padLeft("Monthly Cost", colCost)),
	)

	tree := buildModuleTree(data.ResourceCostEstimates)
	renderBreakdownNode(w, tree, 0, "", colName, colQty, colUnit, colCost)

	renderBreakdownFooter(w, data, colName+colQty+colUnit+2, colCost, projectLabel)
	renderAssumptions(w, data.Diagnostics, false)
}

func filterVisibleSuggestions(suggestions []*api.UpgradeSuggestion) []*api.UpgradeSuggestion {
	if GravitonEnabled {
		return suggestions
	}
	var filtered []*api.UpgradeSuggestion
	for _, s := range suggestions {
		if s.Category != "graviton" {
			filtered = append(filtered, s)
		}
	}
	return filtered
}

func suggestionResource(s *api.UpgradeSuggestion) string {
	resource := resourceLabel(s.ResourceType, s.ResourceName)
	if mp := derefStr(s.ModulePath); mp != "" {
		resource = mp + "." + resource
	}
	return resource
}

// suggestionLocation is where the value to change is written: file:line for a
// CloudFormation template, the file for Terraform.
func suggestionLocation(s *api.UpgradeSuggestion) string {
	if s.Location != nil {
		return fmt.Sprintf("%s:%d", s.Location.File, s.Location.Line)
	}
	return s.File
}

func anyLocated(suggestions []*api.UpgradeSuggestion) bool {
	for _, s := range suggestions {
		if s.Location != nil {
			return true
		}
	}
	return false
}

func renderSuggestionsTable(w io.Writer, suggestions []*api.UpgradeSuggestion) {
	suggestions = filterVisibleSuggestions(suggestions)
	if len(suggestions) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s\n\n", applyStyle(planBold, fmt.Sprintf("Optimization opportunities (%d):", len(suggestions))))

	located := anyLocated(suggestions)
	var rows [][]string
	lastResource := ""
	for _, s := range suggestions {
		resource := suggestionResource(s)
		displayResource := resource
		if resource == lastResource {
			displayResource = ""
		} else {
			lastResource = resource
		}
		savings := "Better performance, same cost"
		if s.EstimatedMonthlySavings != nil && *s.EstimatedMonthlySavings != 0 {
			savings = formatCost(*s.EstimatedMonthlySavings) + "/mo"
		}
		row := []string{displayResource, s.Reason, savings}
		if located {
			row = append(row, suggestionLocation(s))
		}
		rows = append(rows, row)
	}
	headers := []string{"Resource", "Suggestion", "Est. Savings"}
	if located {
		headers = append(headers, "Location")
	}
	TableTo(w, headers, rows)
}

// renderAssumptions lists what the estimate assumed, such as a parameter priced
// at its default.
func renderAssumptions(w io.Writer, diagnostics []*api.Diagnostic, markdown bool) {
	if len(diagnostics) == 0 {
		return
	}
	if markdown {
		fmt.Fprintf(w, "\n**Assumptions**\n\n")
	} else {
		fmt.Fprintf(w, "\n%s\n", applyStyle(planBold, "Assumptions:"))
	}
	for _, d := range diagnostics {
		where := ""
		if d.File != nil {
			where = " (" + *d.File
			if d.Line != nil {
				where += fmt.Sprintf(":%d", *d.Line)
			}
			where += ")"
		}
		if markdown {
			fmt.Fprintf(w, "- %s%s\n", d.Message, where)
		} else {
			fmt.Fprintf(w, "%s\n", applyStyle(planDim, "∙ "+d.Message+where))
		}
	}
}

func renderBreakdownFooter(w io.Writer, data *api.AnalyzePrResponse, labelWidth, costWidth int, projectLabel string) {
	totalCost := float64(0)
	estimable := 0
	total := 0
	if data.CostSummary != nil {
		totalCost = data.CostSummary.TotalNewMonthly
		estimable = data.CostSummary.EstimableCount
		total = data.CostSummary.TotalCount
	}

	fmt.Fprintln(w)
	label := padRight("PROJECT TOTAL", labelWidth)
	fmt.Fprintf(w, " %s%s\n", applyStyle(planBold, label), applyStyle(planBold, padLeft(formatCost(totalCost), costWidth)))

	if total > 0 {
		free := 0
		if data.CostSummary != nil {
			free = derefInt(data.CostSummary.FreeCount)
		}
		unsupported := total - estimable - free
		lines := fmt.Sprintf("%d cloud resources were detected:\n∙ %d were estimated", total, estimable)
		if free > 0 {
			lines += fmt.Sprintf("\n∙ %d were free (no usage cost)", free)
		}
		if unsupported > 0 {
			lines += fmt.Sprintf("\n∙ %d are not yet supported", unsupported)
		}
		fmt.Fprintf(w, "\n%s\n", applyStyle(planDim, lines))
	}

	if hasEstimatedComponents(data) {
		fmt.Fprintf(w, "%s\n", applyStyle(planDim,
			"*Costs are estimated. Actual usage may vary."))
	}

	fmt.Fprintln(w)
	TableTo(w,
		[]string{"Project", "Baseline cost", "Total cost"},
		[][]string{{projectLabel, formatCost(totalCost), formatCost(totalCost)}},
	)

	if len(data.UpgradeSuggestions) > 0 {
		renderSuggestionsTable(w, data.UpgradeSuggestions)
		visible := filterVisibleSuggestions(data.UpgradeSuggestions)
		totalSavings := 0.0
		for _, s := range visible {
			if s.EstimatedMonthlySavings != nil {
				totalSavings += *s.EstimatedMonthlySavings
			}
		}
		if totalSavings > 0 {
			fmt.Fprintf(w, "\n  %s\n", applyStyle(planGreen, "Total potential savings: "+formatCost(totalSavings)+"/mo"))
		}
	}
}

func RenderDiff(w io.Writer, data *api.AnalyzePrResponse, projectLabel string) {
	if QuietMode {
		return
	}
	data, projectLabel = sanitizedResponse(data), Sanitize(projectLabel)

	fmt.Fprintf(w, "\n%s %s\n", applyStyle(planBold, "Project:"), projectLabel)
	fmt.Fprintf(w, "\n%s\n", applyStyle(planDim, "Key: ~ changed, + added, - removed"))

	tree := buildModuleTree(data.ResourceCostEstimates)
	renderDiffNode(w, tree, 0)

	renderDiffSummary(w, data, projectLabel)
	renderAssumptions(w, data.Diagnostics, false)
}

func renderDiffNode(w io.Writer, node *moduleNode, depth int) {
	indent := strings.Repeat("  ", depth)
	if node.path != "" {
		fmt.Fprintf(w, "\n%s%s\n", indent, applyStyle(planDim, node.path))
	}

	for _, e := range node.resources {
		renderDiffResource(w, e, indent)
	}

	for _, child := range node.children {
		renderDiffNode(w, child, depth+1)
	}
}

func renderDiffResource(w io.Writer, e *api.ResourceCostEstimate, indent string) {
	if e.ChangeType == "noop" {
		return
	}
	if isFreeResource(e) || isUnsupportedResource(e) {
		return
	}

	diff := ptrF(e.MonthlyCostDifference)
	if diff == 0 && e.ChangeType == changeModified {
		return
	}

	sym, style := symbolAndStyle(e.ChangeType)
	fmt.Fprintf(w, "\n%s%s %s\n", indent, applyStyle(style, sym), applyStyle(planBold, resourceLabel(e.ResourceType, e.ResourceName)))

	prev := ptrF(e.PreviousMonthlyCost)
	cur := ptrF(e.NewMonthlyCost)
	rangeStr := applyStyle(planDim, fmt.Sprintf("(%s -> %s)", formatCost(prev), formatCost(cur)))
	fmt.Fprintf(w, "%s  %s %s\n", indent, formatDeltaCost(diff), rangeStr)

	topLevel, subGroups := groupBySubresource(changedComponents(e.Components, e.ChangeType))
	for _, c := range topLevel {
		renderDiffComponent(w, c, e.ChangeType, indent)
	}
	for _, g := range subGroups {
		fmt.Fprintf(w, "\n%s    %s\n", indent, applyStyle(planBold, g.name))
		for _, c := range g.components {
			renderDiffComponent(w, c, e.ChangeType, indent+"  ")
		}
	}
}

// componentChange is how one component of a resource changed, with what it
// cost before and after. A removed resource's components carry the cost they
// had as monthly_cost.
func componentChange(c *api.CostComponentEstimate, resourceChange string) (change string, before, after float64) {
	if resourceChange == changeRemoved {
		return changeRemoved, c.MonthlyCost, 0
	}
	before = ptrF(c.PreviousMonthlyCost)
	switch {
	case c.PreviousMonthlyCost == nil:
		return changeAdded, 0, c.MonthlyCost
	case c.MonthlyCost == 0 && before > 0:
		return changeRemoved, before, 0
	}
	return changeModified, before, c.MonthlyCost
}

// changedComponents leaves out the components whose cost did not move, such as
// IOPS a volume type includes.
func changedComponents(components []*api.CostComponentEstimate, resourceChange string) []*api.CostComponentEstimate {
	var changed []*api.CostComponentEstimate
	for _, c := range components {
		if _, before, after := componentChange(c, resourceChange); before != after {
			changed = append(changed, c)
		}
	}
	return changed
}

func renderDiffComponent(w io.Writer, c *api.CostComponentEstimate, resourceChange, indent string) {
	change, before, after := componentChange(c, resourceChange)
	sym, style := symbolAndStyle(change)
	fmt.Fprintf(w, "\n%s    %s %s\n", indent, applyStyle(style, sym), c.Name)

	delta := formatDeltaCost(after - before)
	if c.PreviousMonthlyCost != nil {
		compRange := applyStyle(planDim, fmt.Sprintf("(%s -> %s)", formatCost(before), formatCost(after)))
		fmt.Fprintf(w, "%s      %s %s\n", indent, delta, compRange)
		return
	}
	fmt.Fprintf(w, "%s      %s\n", indent, delta)
}

func renderDiffSummary(w io.Writer, data *api.AnalyzePrResponse, projectLabel string) {
	totalDiff := float64(0)
	totalPrev := float64(0)
	totalNew := float64(0)
	if data.CostSummary != nil {
		totalDiff = data.CostSummary.TotalMonthlyDifference
		totalPrev = data.CostSummary.TotalPreviousMonthly
		totalNew = data.CostSummary.TotalNewMonthly
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s\n", applyStyle(planBold, fmt.Sprintf("Monthly cost change for %s", projectLabel)))
	rangeStr := applyStyle(planDim, fmt.Sprintf("(%s -> %s)", formatCost(totalPrev), formatCost(totalNew)))
	fmt.Fprintf(w, "Amount:  %s %s\n", formatDeltaCost(totalDiff), rangeStr)
	if totalPrev > 0 {
		pct := (totalDiff / totalPrev) * 100
		sign := "+"
		if pct < 0 {
			sign = ""
		}
		fmt.Fprintf(w, "Percent: %s%.0f%%\n", sign, pct)
	}

	if data.CostSummary != nil && data.CostSummary.TotalCount > 0 {
		free := derefInt(data.CostSummary.FreeCount)
		unsupported := data.CostSummary.TotalCount - data.CostSummary.EstimableCount - free
		lines := fmt.Sprintf("%d cloud resources were detected:\n∙ %d were estimated", data.CostSummary.TotalCount, data.CostSummary.EstimableCount)
		if free > 0 {
			lines += fmt.Sprintf("\n∙ %d were free (no usage cost)", free)
		}
		if unsupported > 0 {
			lines += fmt.Sprintf("\n∙ %d are not yet supported", unsupported)
		}
		fmt.Fprintf(w, "\n%s\n", applyStyle(planDim, lines))
	}
	if hasEstimatedComponents(data) {
		fmt.Fprintf(w, "\n%s\n", applyStyle(planDim,
			"*Costs are estimated. Actual usage may vary."))
	}

	if len(data.UpgradeSuggestions) > 0 {
		renderSuggestionsTable(w, data.UpgradeSuggestions)
	}
}

func renderMarkdownDetectionSummary(w io.Writer, summary *api.CostSummary) {
	if summary == nil || summary.TotalCount == 0 {
		return
	}
	free := derefInt(summary.FreeCount)
	unsupported := summary.TotalCount - summary.EstimableCount - free
	fmt.Fprintf(w, "\n%d cloud resources were detected:", summary.TotalCount)
	fmt.Fprintf(w, "\n- %d were estimated", summary.EstimableCount)
	if free > 0 {
		fmt.Fprintf(w, "\n- %d were free", free)
	}
	if unsupported > 0 {
		fmt.Fprintf(w, "\n- %d are not yet supported", unsupported)
	}
	fmt.Fprintln(w)
}

// markdownGroup is the root, nested stack and module a resource sits in.
func markdownGroup(e *api.ResourceCostEstimate, byScope bool) string {
	var parts []string
	if byScope && derefStr(e.Scope) != "" {
		parts = append(parts, "`"+derefStr(e.Scope)+"`")
	}
	if mp := derefStr(e.ModulePath); mp != "" {
		parts = append(parts, "`"+mp+"`")
	}
	return strings.Join(parts, " ")
}

func renderMarkdownTable(w io.Writer, data *api.AnalyzePrResponse) bool {
	byScope := distinctScopes(data.ResourceCostEstimates) > 1
	hasGroups := false
	for _, e := range data.ResourceCostEstimates {
		if markdownGroup(e, byScope) != "" {
			hasGroups = true
			break
		}
	}

	fmt.Fprintln(w)
	if hasGroups {
		fmt.Fprintln(w, "| Location | Resource | Type | Cost/mo | Delta |")
		fmt.Fprintln(w, "|----------|----------|------|---------|-------|")
	} else {
		fmt.Fprintln(w, "| Resource | Type | Cost/mo | Delta |")
		fmt.Fprintln(w, "|----------|------|---------|-------|")
	}
	for _, e := range data.ResourceCostEstimates {
		if isFreeResource(e) || isUnsupportedResource(e) {
			continue
		}
		sym, _ := symbolAndStyle(e.ChangeType)
		cost := ptrF(e.NewMonthlyCost)
		diff := ptrF(e.MonthlyCostDifference)
		if hasGroups {
			fmt.Fprintf(w, "| %s | %s %s | %s | %s | %s |\n",
				markdownGroup(e, byScope), sym, e.ResourceName, e.ResourceType, formatCost(cost), formatDeltaCost(diff))
		} else {
			fmt.Fprintf(w, "| %s %s | %s | %s | %s |\n",
				sym, e.ResourceName, e.ResourceType, formatCost(cost), formatDeltaCost(diff))
		}
	}
	return hasGroups
}

func RenderMarkdown(w io.Writer, data *api.AnalyzePrResponse, isDiff bool) {
	data = sanitizedResponse(data)
	if isDiff {
		fmt.Fprintln(w, "## Cost Estimate (diff)")
	} else {
		fmt.Fprintln(w, "## Cost Estimate")
	}

	renderMarkdownTable(w, data)

	totalNew := float64(0)
	totalDiff := float64(0)
	if data.CostSummary != nil {
		totalNew = data.CostSummary.TotalNewMonthly
		totalDiff = data.CostSummary.TotalMonthlyDifference
	}

	fmt.Fprintf(w, "\n**Monthly estimate:** %s\n", formatCost(totalNew))
	if totalDiff != 0 {
		fmt.Fprintf(w, "**Delta:** %s/mo\n", formatDeltaCost(totalDiff))
	}

	renderMarkdownDetectionSummary(w, data.CostSummary)

	if hasEstimatedComponents(data) {
		fmt.Fprintf(w, "\n*Costs are estimated. Actual usage may vary.*\n")
	}

	visible := filterVisibleSuggestions(data.UpgradeSuggestions)
	if len(visible) > 0 {
		renderMarkdownSuggestions(w, visible)
	}
	renderAssumptions(w, data.Diagnostics, true)
}

func renderMarkdownSuggestions(w io.Writer, suggestions []*api.UpgradeSuggestion) {
	located := anyLocated(suggestions)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<details><summary>Optimization suggestions</summary>")
	fmt.Fprintln(w)
	if located {
		fmt.Fprintln(w, "| Resource | Suggestion | Est. Savings | Location |")
		fmt.Fprintln(w, "|----------|------------|-------------|----------|")
	} else {
		fmt.Fprintln(w, "| Resource | Suggestion | Est. Savings |")
		fmt.Fprintln(w, "|----------|------------|-------------|")
	}
	for _, s := range suggestions {
		savings := ""
		if s.EstimatedMonthlySavings != nil {
			savings = formatCost(*s.EstimatedMonthlySavings) + "/mo"
		}
		if located {
			fmt.Fprintf(w, "| %s | %s | %s | `%s` |\n", suggestionResource(s), s.Reason, savings, suggestionLocation(s))
		} else {
			fmt.Fprintf(w, "| %s | %s | %s |\n", suggestionResource(s), s.Reason, savings)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "</details>")
}
