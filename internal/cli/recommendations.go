package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var flagTUI bool

var (
	flagListTUI         bool
	flagRecsProvider    string
	flagRecsService     []string
	flagRecsEnvironment []string
	flagRecsAccount     []string
	flagRecsTag         []string
	flagRecsStatus      []string
	flagRecsSearch      string
	flagRecsSortBy      string
	flagRecsSortOrder   string
)

var terminalWidth = func() int {
	if f, ok := output.Stdout.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	return 80
}

var recommendationsCmd = &cobra.Command{
	Use:   "recommendations",
	Short: "View cost optimization recommendations",
}

var recommendationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List cost optimization recommendations",
	Example: `  l4 recommendations list
  l4 recommendations list --service RDS --status available
  l4 recommendations list --sort-by monthly_savings --sort-order desc
  l4 recommendations list --page 2 --page-size 10
  l4 recommendations list --provider gcp --environment production`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb("/savings-recommendations")
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		ctx := context.Background()
		page, _ := cmd.Flags().GetInt("page")
		pageSize, _ := cmd.Flags().GetInt("page-size")

		providerID, err := resolveProviderID(ctx, client, flagRecsProvider)
		if err != nil {
			return err
		}

		req := buildListByProviderRequest(page, pageSize)

		type overviewResult struct {
			data *levelfourgo.RecommendationsOverviewResponse
			err  error
		}
		type listResult struct {
			items    []*levelfourgo.ProviderBreakdownItem
			response *levelfourgo.ProviderBreakdownResponse
			err      error
		}

		var ovr overviewResult
		var lsr listResult
		var wg sync.WaitGroup
		wg.Add(2)

		err = tuicommon.RunWithSpinner("Loading recommendations...", output.L4SpinnerTheme(), func(_ context.Context) error {
			go func() {
				defer wg.Done()
				data, e := client.SDK().Recommendations.GetProviderOverview(ctx, providerID)
				ovr = overviewResult{data, e}
			}()
			go func() {
				defer wg.Done()
				pg, e := client.SDK().Recommendations.ListByProvider(ctx, providerID, req)
				if e != nil {
					lsr = listResult{err: e}
				} else {
					lsr = listResult{items: pg.Results, response: pg.Response}
				}
			}()
			wg.Wait()
			if ovr.err != nil {
				return ovr.err
			}
			return lsr.err
		})
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() {
			return output.PrintResult(map[string]interface{}{
				"overview": ovr.data,
				"data":     lsr.response,
			})
		}

		if flagListTUI && isTerminal() {
			selectedID, tuiErr := runRecommendationsListTUI(
				client, providerID,
				ovr.data.GetData(),
				lsr.items,
				lsr.response.GetData().GetPagination(),
				pageSize,
			)
			if tuiErr != nil {
				return tuiErr
			}
			if selectedID != "" {
				tuicommon.DrainTermInput()
				resp, fetchErr := client.SDK().Recommendations.Get(ctx, selectedID)
				if fetchErr != nil {
					return fetchErr
				}
				dataMap := toMap(resp.GetData())
				return runRecommendationViewTUI(dataMap)
			}
			return nil
		}

		kpi := ovr.data.GetData()
		output.KPICards([]output.KPICard{
			{Label: "Total Spend", Value: fmt.Sprintf("$%.2f/mo", kpi.GetTotalSpend())},
			{Label: "Available Savings", Value: fmt.Sprintf("$%.2f/mo", kpi.GetAvailableSavings())},
			{Label: "Pending Savings", Value: fmt.Sprintf("$%.2f/mo", kpi.GetPendingSavings())},
			{Label: "Saved CTD", Value: fmt.Sprintf("$%.2f/mo", kpi.GetSavedItd())},
		})

		items := lsr.items
		if len(items) == 0 {
			output.Info("No recommendations found.")
			return nil
		}

		headers, rows := buildRecommendationRows(items, terminalWidth())
		output.Table(headers, rows)

		if pg := lsr.response.GetData().GetPagination(); pg != nil {
			output.PaginationFooter(
				pg.GetCurrentPage(),
				pg.GetTotalPages(),
				pg.GetTotalItems(),
				pg.GetHasNext(),
			)
		}

		return nil
	},
}

func resolveProviderID(ctx context.Context, client *api.SDKClient, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	resp, err := client.SDK().Providers.List(ctx)
	if err != nil {
		return "", err
	}
	providers := resp.GetData()
	if len(providers) == 0 {
		return "", fmt.Errorf("no providers connected: use --provider to specify one")
	}
	return providers[0].GetProviderID(), nil
}

func buildListByProviderRequest(page, pageSize int) *levelfourgo.ListByProviderRecommendationsRequest {
	req := &levelfourgo.ListByProviderRecommendationsRequest{
		Page:     api.IntPtr(page),
		PageSize: api.IntPtr(pageSize),
	}
	if len(flagRecsService) > 0 {
		req.Service = flagRecsService
	}
	if len(flagRecsEnvironment) > 0 {
		req.Environment = flagRecsEnvironment
	}
	if len(flagRecsAccount) > 0 {
		req.Account = flagRecsAccount
	}
	if len(flagRecsTag) > 0 {
		req.Tag = flagRecsTag
	}
	if len(flagRecsStatus) > 0 {
		req.DisplayStatus = flagRecsStatus
	}
	if flagRecsSortBy != "" {
		req.SortBy = api.StringPtr(flagRecsSortBy)
	}
	if flagRecsSortOrder != "" {
		so := levelfourgo.ListByProviderRecommendationsRequestSortOrder(flagRecsSortOrder)
		req.SortOrder = &so
	}
	return req
}

type recColumn struct {
	header   string
	minWidth int
	value    func(*levelfourgo.ProviderBreakdownItem) string
}

var recColumns = []recColumn{
	{"ID", 0, func(r *levelfourgo.ProviderBreakdownItem) string { return r.GetRecommendationID() }},
	{"Service", 0, func(r *levelfourgo.ProviderBreakdownItem) string { return r.GetService() }},
	{"Environment", 100, func(r *levelfourgo.ProviderBreakdownItem) string {
		if e := r.GetEnvironment(); e != nil {
			return *e
		}
		return ""
	}},
	{"Account", 140, func(r *levelfourgo.ProviderBreakdownItem) string {
		if a := r.GetAccount(); a != nil {
			return *a
		}
		return ""
	}},
	{"Tag", 160, func(r *levelfourgo.ProviderBreakdownItem) string {
		if t := r.GetTag(); t != nil {
			return *t
		}
		return ""
	}},
	{"Monthly Savings", 0, func(r *levelfourgo.ProviderBreakdownItem) string {
		return fmt.Sprintf("$%.2f", r.GetMonthlySavings())
	}},
	{"Savings %", 100, func(r *levelfourgo.ProviderBreakdownItem) string {
		return fmt.Sprintf("%.1f%%", r.GetSavingsPercentage())
	}},
	{"Status", 0, func(r *levelfourgo.ProviderBreakdownItem) string {
		if s := r.GetStatus(); s != nil {
			return string(*s)
		}
		return ""
	}},
	{"Author", 160, func(r *levelfourgo.ProviderBreakdownItem) string {
		if a := r.GetSavingAcceptedBy(); a != nil {
			return *a
		}
		return ""
	}},
}

func buildRecommendationRows(items []*levelfourgo.ProviderBreakdownItem, width int) ([]string, [][]string) {
	var active []recColumn
	for _, c := range recColumns {
		if width >= c.minWidth {
			active = append(active, c)
		}
	}

	headers := make([]string, len(active))
	for i, c := range active {
		headers[i] = c.header
	}

	var rows [][]string
	for _, item := range items {
		if flagRecsSearch != "" {
			match := false
			search := strings.ToLower(flagRecsSearch)
			for _, c := range recColumns {
				if strings.Contains(strings.ToLower(c.value(item)), search) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		row := make([]string, len(active))
		for i, c := range active {
			row[i] = c.value(item)
		}
		rows = append(rows, row)
	}
	return headers, rows
}

var recommendationsViewCmd = &cobra.Command{
	Use:   "view <id>",
	Short: "View recommendation details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagWeb {
			return openWeb(fmt.Sprintf("/savings-recommendations?recommendation=%s", args[0]))
		}

		ctx := context.Background()

		if flagTUI && isTerminal() {
			c, err := newSDKClientFn()
			if err != nil {
				return err
			}
			resp, fetchErr := c.SDK().Recommendations.Get(ctx, args[0])
			if fetchErr != nil {
				return fetchErr
			}
			dataMap := toMap(resp.GetData())
			return runRecommendationViewTUI(dataMap)
		}

		client, err := newSDKClientFn()
		if err != nil {
			return err
		}

		if output.HasFormattingFlags() || output.QuietMode {
			resp, fetchErr := client.SDK().Recommendations.Get(ctx, args[0])
			if fetchErr != nil {
				return fetchErr
			}
			if output.QuietMode {
				return nil
			}
			return output.PrintResult(resp)
		}

		var resp *levelfourgo.RecommendationDetailResponse

		err = tuicommon.RunWithSpinner("Loading recommendation...", output.L4SpinnerTheme(), func(_ context.Context) error {
			var fetchErr error
			resp, fetchErr = client.SDK().Recommendations.Get(ctx, args[0])
			return fetchErr
		})
		if err != nil {
			return err
		}

		renderRecommendationViewStatic(resp.GetData())
		return nil
	},
}

func toMap(v interface{}) map[string]interface{} {
	b, _ := json.Marshal(v)
	var m map[string]interface{}
	_ = json.Unmarshal(b, &m)
	return m
}

func renderRecommendationViewStatic(data *levelfourgo.RecommendationDetail) {
	output.Header(fmt.Sprintf("Recommendation %v", data.GetRecommendationID()))
	fmt.Fprintln(output.Stdout)
	renderRecommendationMetadata(data)

	fmt.Fprintln(output.Stdout)
	output.Header("Savings")
	output.KeyValue("Current Spending", fmt.Sprintf("$%.2f", data.GetCurrentSpending()))
	output.KeyValue("Monthly Savings", fmt.Sprintf("$%.2f", data.GetMonthlySavings()))
	output.KeyValue("Annual Savings", fmt.Sprintf("$%.2f", data.GetAnnualSavings()))
	if pct := data.GetSavingsPercentage(); pct > 0 {
		output.KeyValue("Savings", fmt.Sprintf("%.1f%%", pct))
	}

	if actions := data.GetActions(); actions != nil {
		renderRecommendationActions(actions)
	}

	renderComparisonData(data.GetComparisonData())
	renderRiskAssessment(data.GetRiskAssessment())
	renderImplementationSteps(data.GetImplementationSteps())
}

func renderRecommendationMetadata(data *levelfourgo.RecommendationDetail) {
	output.KeyValue("Service", data.GetService())
	if v := data.GetResourceType(); v != nil && *v != "" {
		output.KeyValue("Resource Type", *v)
	}
	if v := data.GetAccount(); v != nil && *v != "" {
		output.KeyValue("Account", *v)
	}
	if v := data.GetRegion(); v != nil && *v != "" {
		output.KeyValue("Region", *v)
	}
	env := ""
	if data.GetEnvironment() != nil {
		env = *data.GetEnvironment()
	}
	output.KeyValue("Environment", env)
	if s := data.GetStatus(); s != nil {
		output.KeyValue("Status", output.StatusBadge(string(*s)))
	}
	if ap := data.GetAnalysisPeriod(); ap != nil && *ap != "" {
		output.KeyValue("Analysis Period", formatDate(*ap))
	}
	if ca := data.GetCreatedAt(); ca != nil && *ca != "" {
		output.KeyValue("Created", formatDate(*ca))
	}
	if v := data.GetResourceConsoleURL(); v != nil && *v != "" {
		output.KeyValue("Console URL", *v)
	}
}

func renderRecommendationActions(actions *levelfourgo.RecommendationActions) {
	printSection := func(header string, val *string) {
		if val != nil && *val != "" {
			fmt.Fprintln(output.Stdout)
			output.Header(header)
			output.Markdown(*val)
		}
	}

	if desc := actions.GetDescription(); desc != nil && *desc != "" {
		fmt.Fprintln(output.Stdout)
		output.Header("Description")
		fmt.Fprintf(output.Stdout, "  %s\n", *desc)
	}
	printSection("Key Takeaway", actions.GetKeyTakeaway())
	printSection("Operational Impact", actions.GetOperationalImpact())
	printSection("Implementation", actions.GetImplementationProcess())
	printSection("Execution Method", actions.GetExecutionMethod())
}

func renderComparisonData(items []any) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintln(output.Stdout)
	output.Header("Comparison")
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		label, _ := m["label"].(string)
		cur, _ := m["current_value"].(string)
		nv, _ := m["new_value"].(string)
		fmt.Fprintf(output.Stdout, "  %-30s %s → %s\n", label, cur, nv)
	}
}

func renderRiskAssessment(raw any) {
	ra, ok := raw.(map[string]interface{})
	if !ok {
		return
	}
	fmt.Fprintln(output.Stdout)
	level, _ := ra["level"].(string)
	output.Header(fmt.Sprintf("Risk Assessment (%s)", level))
	factors, _ := ra["factors"].([]interface{})
	for _, f := range factors {
		fm, ok := f.(map[string]interface{})
		if !ok {
			continue
		}
		factor, _ := fm["factor"].(string)
		detail, _ := fm["detail"].(string)
		fmt.Fprintf(output.Stdout, "  • %s\n    %s\n", factor, detail)
	}
}

func renderImplementationSteps(steps []any) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(output.Stdout)
	output.Header("Implementation Steps")
	for i, s := range steps {
		sm, ok := s.(map[string]interface{})
		if !ok {
			continue
		}
		action, _ := sm["action"].(string)
		detail, _ := sm["detail"].(string)
		fmt.Fprintf(output.Stdout, "  %d. %s\n     %s\n", i+1, action, detail)
	}
}

func init() {
	recommendationsListCmd.Flags().Int("page", 1, "Page number")
	recommendationsListCmd.Flags().Int("page-size", 20, "Items per page")
	recommendationsListCmd.Flags().StringVar(&flagRecsProvider, "provider", "", "Provider ID (aws, gcp, azure, k8s); auto-detected if omitted")
	recommendationsListCmd.Flags().StringArrayVar(&flagRecsService, "service", nil, "Filter by service (repeatable)")
	recommendationsListCmd.Flags().StringArrayVar(&flagRecsEnvironment, "environment", nil, "Filter by environment (repeatable)")
	recommendationsListCmd.Flags().StringArrayVar(&flagRecsAccount, "account", nil, "Filter by account (repeatable)")
	recommendationsListCmd.Flags().StringArrayVar(&flagRecsTag, "tag", nil, "Filter by tag (repeatable)")
	recommendationsListCmd.Flags().StringArrayVar(&flagRecsStatus, "status", nil, "Filter by status: available, pending, processing, optimized, rejected, unavailable (repeatable)")
	recommendationsListCmd.Flags().StringVar(&flagRecsSearch, "search", "", "Search within current page results")
	recommendationsListCmd.Flags().StringVar(&flagRecsSortBy, "sort-by", "", "Sort by: recommendation_id, service, environment, account, tag, monthly_savings, savings_percentage, status")
	recommendationsListCmd.Flags().StringVar(&flagRecsSortOrder, "sort-order", "", "Sort order: asc, desc")
	recommendationsListCmd.Flags().BoolVar(&flagListTUI, "tui", false, "Use interactive TUI viewer")

	recommendationsViewCmd.Flags().BoolVar(&flagTUI, "tui", false, "Use interactive TUI viewer")

	recommendationsCmd.AddCommand(recommendationsListCmd)
	recommendationsCmd.AddCommand(recommendationsViewCmd)
}
