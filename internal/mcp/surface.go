// Package mcp serves the LevelFour MCP tool surface over stdio, backed by the
// LevelFour REST API and the credential the CLI already stores.
//
// The hosted server at https://mcp.levelfour.ai/mcp serves the same tools over
// streamable HTTP, plus two that record a decision and are shown only to a
// read-write credential. Everything in this file mirrors the hosted server's own
// tool definitions, and the two must not drift: a model that learned to
// route across the hosted tools has to get the same answer from a name it saw
// there. TestSurfaceMatchesTheCatalogForThisCredential pins these names against a
// vendored copy of the hosted catalog and fails on anything this side invents or
// leaves out.
package mcp

import (
	"encoding/json"
	"strings"
)

// Serving locally is the fallback for a client that cannot send an
// Authorization header to a remote server.
const Endpoint = "https://mcp.levelfour.ai/mcp"

// Reported during initialize, and the default entry name in a client's config.
const ServerName = "levelfour"

// The hosted instructions, with the write-tool paragraph rewritten for a surface
// that does not serve them. It names `l4 rec accept` rather than the tool it
// stands in for, which a model here has no way to reach.
const Instructions = "Answers questions about a LevelFour organization's cloud spend, savings opportunities " +
	"and realized savings. Every tool here is read-only, and this server serves the sixteen read " +
	"tools of the hosted server's catalog. The hosted server additionally records a user's decision " +
	"to accept or reject a recommendation, which needs a read-write credential and is not available " +
	"here: on this surface, tell the user to run `l4 rec accept <id>` or `l4 rec reject <id>`. " +
	"Deciding is not applying. Applying an accepted change happens in the LevelFour dashboard or " +
	"with `l4 rec execute`. Potential savings and realized savings are different quantities and " +
	"must never be added together."

// There is no exception list on purpose. Descriptions name their neighbours, so
// a tool dropped here sends a model to a dead end. When a REST route refuses the
// key this CLI holds, open the route rather than quietly dropping the tool.

// Resources are the one hosted feature this server leaves behind, and the reason
// is narrower than an API gap: the routes behind them are reachable with the
// credential this server already holds. What is missing is the provider
// resolution the hosted resources do, each resolving one provider and falling
// back to the organization's first connected one, which this server has no
// equivalent of. The three hosted prompts are ported in prompts.go.

type tool struct {
	name        string
	description string
	schema      json.RawMessage

	// calls are the REST requests this tool makes, in order. One call with an
	// empty key returns its payload as the whole result; anything else nests
	// each payload under its key.
	calls []call

	// altArg names an argument whose absence swaps calls for altCalls. Only
	// costs_by_tag needs it: with no tag_key it lists the keys instead.
	altArg   string
	altCalls []call

	// echo copies these arguments into the result, so a caller can tell which
	// tag key produced the numbers without holding onto its own request.
	echo []string

	// shape rewrites the assembled result. Reserved for the two places the REST
	// payload and the hosted payload genuinely differ.
	shape func(map[string]any) map[string]any
}

type call struct {
	key   string // result key; empty lifts this call's payload to the top level
	pick  string // sub-key of the payload to use in place of the whole payload
	path  string // REST path, with {arg} placeholders filled from the arguments
	query []binding

	rows    string // items key, for pagination, the empty hint and the size budget
	rowsAlt string // fallback items key when rows is absent from the payload
	hint    string // attached when rows comes back empty
	paged   bool   // re-shape onto the shared pagination envelope
	bound   bool   // cap an unpaginated list the REST route returns whole
}

type binding struct {
	param string // query parameter name on the REST side
	arg   string // tool argument name on the MCP side
	def   any    // sent when the argument is absent; nil omits the parameter
	list  bool   // repeat the parameter once per element
}

func lines(s ...string) string { return strings.Join(s, "\n") }

// Mirrors the Annotated aliases in the hosted definitions, which is why sixteen
// tools agree on what "page_size" means.

const (
	argProvider  = "provider"
	argPage      = "page"
	argPageSize  = "page_size"
	argSortBy    = "sort_by"
	argSortOrder = "sort_order"
	argStart     = "start"
	argEnd       = "end"
	argTagKey    = "tag_key"
	argLimit     = "limit"
	argService   = "service"
	argEnv       = "environment"
	argStatus    = "status"
	argSeverity  = "severity"
	rowsItems    = "items"
	rowsServices = "services"
	sortByDesc   = "Field to sort by."
	defaultAWS   = "aws"
)

const providerDescription = "Cloud provider id, for example aws, gcp, azure or k8s. Omit for every provider this " +
	"organization has. Call get-identity if you are unsure which are available."

func providerProp() prop { return optStringProp(argProvider, providerDescription) }
func pageProp() prop     { return intProp(argPage, "1-indexed page number.", 1) }
func pageSizeProp() prop {
	return boundedIntProp(argPageSize, "Rows per page.", defaultPageSize)
}
func limitProp() prop {
	return boundedIntProp(argLimit, "Maximum rows to return.", defaultPageSize)
}
func sortOrderProp() prop { return enumProp(argSortOrder, "Sort direction.", "desc", "asc", "desc") }
func startProp(n string) prop {
	return optDateProp(n, "Start of the range, YYYY-MM-DD. Defaults to this month.")
}
func endProp(n string) prop {
	return optDateProp(n, "End of the range, YYYY-MM-DD. Defaults to today.")
}
func filterProp(n string) prop { return optListProp(n, "Restrict to these values. Omit for all.") }

// Defaulted to aws, because most REST cost routes demand one.
func providerQuery() binding {
	return binding{param: argProvider, arg: argProvider, def: defaultAWS}
}

func pagingQuery() []binding {
	return []binding{
		{param: argPage, arg: argPage, def: 1},
		{param: argPageSize, arg: argPageSize, def: defaultPageSize},
	}
}

// In the hosted server's own READ_TOOLS order.
var tools = []tool{
	{
		name: "get-identity",
		description: lines(
			"**Purpose:** Identify the connected LevelFour organization and what it has access to.",
			"",
			"**Returns:** the organization, its plan, your role, and the cloud providers that have data.",
			"**When to use:** To check the connection works, or when a provider argument was rejected and you",
			"need the valid values. You do not need to call this before every analysis.",
			"**NOT for:** Cloud account ids or names, use `list-accounts`. Spend figures, use",
			"`get-cost-summary`.",
		),
		schema: object("get_identityArguments"),
		calls:  []call{{path: "/api/v1/auth/whoami"}},
		shape:  shapeWhoami,
	},
	{
		name: "list-accounts",
		description: lines(
			"**Purpose:** The cloud accounts connected to this organization.",
			"",
			"**Returns:** account id, name, provider, region, connection status, and whether detailed billing",
			"data is flowing for each.",
			"**When to use:** To turn an account id seen in a cost or recommendation row into a name, to answer",
			"\"which accounts are connected\", or to check why an account has no data.",
			"**NOT for:** Spend per account, use `get-cost-breakdown` with group_by=\"account_id\".",
		),
		schema: object("list_accountsArguments", pageProp(), pageSizeProp()),
		calls: []call{{
			path:  "/api/v1/accounts",
			query: pagingQuery(),
			rows:  rowsItems,
			paged: true,
			hint: "No cloud accounts are connected yet. Connect one in the LevelFour dashboard under " +
				"Settings.",
		}},
	},
	{
		name: "get-cost-summary",
		description: lines(
			"**Purpose:** Current cloud spend and where it is heading.",
			"",
			"**Returns:** total spend, per-provider totals, month-over-month change, and the monthly series.",
			"**When to use:** The opening question, \"what are we spending\". Start here before drilling in.",
			"**NOT for:** Spend split by service, account or tag, use `get-cost-breakdown`. A day-level",
			"series, use `get-cost-series`.",
		),
		schema: object("get_cost_summaryArguments"),
		calls: []call{
			{key: "summary", path: "/api/v1/costs/summary"},
			{key: "monthly", path: "/api/v1/costs/monthly-spending"},
		},
	},
	{
		name: "get-cost-breakdown",
		description: lines(
			"**Purpose:** Break spend down into its largest components over a period.",
			"",
			"**Returns:** a paginated, sorted list of cost lines with amounts and shares.",
			"**When to use:** After `get-cost-summary`, to answer \"what is driving it\". Ask one broad",
			"question first rather than one call per service.",
			"**Examples:** top services this month: no arguments. Largest accounts in July: period=\"2026-07\",",
			"sort_by=\"account_id\".",
			"**NOT for:** Explaining a change over time, use `get-cost-growth`. Dating when a jump",
			"started, use `get-cost-series`. Savings opportunities, use `list-recommendations`.",
		),
		schema: object("get_cost_breakdownArguments",
			providerProp(),
			optStringProp("period", "Month to report on, YYYY-MM. Omit to use start_date and end_date."),
			startProp("start_date"),
			endProp("end_date"),
			pageProp(),
			pageSizeProp(),
			enumProp(argSortBy, sortByDesc, "cost", "cost", "service", "region", "account_id", "change_percentage"),
			sortOrderProp(),
		),
		calls: []call{{
			path: "/api/v1/costs/breakdown",
			query: append(pagingQuery(),
				binding{param: "provider_id", arg: argProvider},
				binding{param: "period", arg: "period"},
				binding{param: argStart, arg: "start_date"},
				binding{param: argEnd, arg: "end_date"},
				binding{param: argSortBy, arg: argSortBy, def: "cost"},
				binding{param: argSortOrder, arg: argSortOrder, def: "desc"},
			),
			rows:  rowsItems,
			paged: true,
			hint: "No cost rows in that window. Widen the date range, drop the provider filter, or call " +
				"get-cost-summary to see which months have data.",
		}},
	},
	{
		name: "get-cost-series",
		description: lines(
			"**Purpose:** Daily spend over a date range, as a time series.",
			"",
			"**Returns:** one point per day with the total for that day.",
			"**When to use:** To date a step change. When the bill jumped and you need to know which day it",
			"started, this is the only tool that can tell you. Pair it with `get-cost-breakdown` over",
			"the same window to attribute the jump.",
			"**NOT for:** Month totals, use `get-cost-summary`. Splitting a day by service, use",
			"`get-cost-breakdown` for that window.",
		),
		schema: object("get_cost_seriesArguments", startProp(argStart), endProp(argEnd)),
		calls: []call{{
			path: "/api/v1/costs/daily/breakdown",
			query: []binding{
				{param: argStart, arg: argStart},
				{param: argEnd, arg: argEnd},
			},
			rows:    "daily_spending",
			rowsAlt: rowsItems,
			hint: "No daily data in that range. Detailed billing data may not be ingested yet for this " +
				"organization; list-accounts shows the state per account.",
		}},
	},
	{
		name: "get-cost-growth",
		description: lines(
			"**Purpose:** Which services grew the most, and by how much.",
			"",
			"**Returns:** services ranked by absolute and percentage increase, plus the two windows compared",
			"and how many days each covers, so a partial month is visible rather than silent.",
			"**When to use:** \"Why did the bill go up.\" Keep the default compare_to unless the user asked",
			"specifically about calendar months.",
			"**NOT for:** Absolute spend ranking, use `get-cost-breakdown`. Unexpected spikes",
			"specifically, use `list-anomalies`.",
		),
		schema: object("get_cost_growthArguments",
			providerProp(),
			enumProp("compare_to",
				"Baseline window. 'previous_period' compares the last 30 days against the 30 before "+
					"them and is the reliable default. 'previous_month' compares this month so far "+
					"against the whole of last month, so early in a month it understates growth.",
				"previous_period", "previous_period", "previous_month"),
			limitProp(),
		),
		calls: []call{{
			path: "/api/v1/costs/top-growing",
			query: []binding{
				providerQuery(),
				{param: "compare_to", arg: "compare_to", def: "previous_period"},
				{param: argLimit, arg: argLimit, def: defaultPageSize},
			},
			rows:    rowsItems,
			rowsAlt: rowsServices,
		}},
	},
	{
		name: "get-cost-forecast",
		description: lines(
			"**Purpose:** Projected spend for the end of the month or the next 30 or 90 days.",
			"",
			"**Returns:** the forecast figure and the basis it was derived from.",
			"**When to use:** \"Where will we land this month.\"",
			"**NOT for:** Historical spend, use `get-cost-summary`. LevelFour has no budget object, so",
			"if the user asks whether they will exceed a budget you must ask them for the number.",
		),
		schema: object("get_cost_forecastArguments",
			providerProp(),
			enumProp("horizon", "Forecast horizon: end of month, next 30 days, or next 90 days.",
				"eom", "eom", "30d", "90d"),
		),
		calls: []call{{
			path: "/api/v1/costs/forecast",
			query: []binding{
				providerQuery(),
				{param: "horizon", arg: "horizon", def: "eom"},
			},
		}},
	},
	{
		name: "get-costs-by-tag",
		description: lines(
			"**Purpose:** Spend grouped by the value of a cost-allocation tag, for showback and chargeback.",
			"",
			"**Returns:** spend per tag value, plus allocation coverage showing how much spend carries the tag.",
			"Called with no tag_key, returns the tag keys available so you can pick one.",
			"**When to use:** \"What does team X cost\", or any per-team, per-environment or per-owner question.",
			"Read the coverage figure before reporting a total: untagged spend is not zero spend.",
			"**Examples:** discover keys: no arguments. Then: tag_key=\"team\".",
			"**NOT for:** Per-service or per-account splits, use `get-cost-breakdown`. Waste per team,",
			"which no tool can answer directly because recommendations do not carry tags.",
		),
		schema: object("get_costs_by_tagArguments",
			optStringProp(argTagKey,
				"Cost allocation tag key to group by. Call with no tag_key to list the keys available."),
			providerProp(),
			startProp(argStart),
			endProp(argEnd),
		),
		altArg: argTagKey,
		altCalls: []call{{
			key:   "available_tag_keys",
			pick:  "tag_keys",
			path:  "/api/v1/costs/by-tag/keys",
			query: []binding{providerQuery()},
			rows:  "available_tag_keys",
			hint: "No cost allocation tags are active for this provider. They must be activated in the " +
				"cloud provider's billing console before spend can be grouped by them.",
		}},
		echo: []string{argTagKey},
		calls: []call{
			{
				key:  "by_tag",
				path: "/api/v1/costs/by-tag",
				query: []binding{
					providerQuery(),
					{param: argTagKey, arg: argTagKey},
					{param: argStart, arg: argStart},
					{param: argEnd, arg: argEnd},
				},
				rows:  rowsItems,
				bound: true,
			},
			{
				key:  "allocation",
				path: "/api/v1/costs/allocation",
				query: []binding{
					providerQuery(),
					{param: argTagKey, arg: argTagKey},
					{param: argStart, arg: argStart},
					{param: argEnd, arg: argEnd},
				},
			},
		},
	},
	{
		name: "get-usage-costs",
		description: lines(
			"**Purpose:** Billable quantity and cost per unit, grouped by usage type.",
			"",
			"**Returns:** spend, quantity, unit and derived unit cost per usage type, largest first.",
			"**When to use:** To ground a savings claim in measured consumption, or to tell a price change",
			"apart from a volume change.",
			"**NOT for:** Spend totals, use `get-cost-breakdown`. Note that usage quantity, not cost,",
			"is the reliable idleness signal here, because the cost columns round to cents.",
		),
		schema: object("get_usage_costsArguments", providerProp(), startProp(argStart), endProp(argEnd)),
		calls: []call{{
			path: "/api/v1/costs/usage",
			query: []binding{
				// "all" rather than aws: the hosted tool resolves an omitted
				// provider across every provider the tenant has.
				{param: argProvider, arg: argProvider, def: "all"},
				{param: argStart, arg: argStart},
				{param: argEnd, arg: argEnd},
			},
			rows:  rowsItems,
			bound: true,
		}},
	},
	{
		name: "list-recommendations",
		description: lines(
			"**Purpose:** Savings opportunities LevelFour has found, largest first.",
			"",
			"**Returns:** a paginated list with id, service, environment, account, monthly and annual savings,",
			"status and a short description. Full detail is deliberately omitted.",
			"**When to use:** \"What can we save.\" Filter server-side rather than paging and discarding: to",
			"answer \"what can we save on RDS in staging\", pass service and environment.",
			"**Examples:** biggest wins: no arguments. Everything actionable without a human:",
			"available_formats=[\"automated\"].",
			"**NOT for:** Savings already realized, use `get-realized-savings`. These are potential and",
			"unbooked, and the two must never be added together.",
		),
		schema: object("list_recommendationsArguments",
			providerProp(),
			filterProp(argService),
			filterProp(argEnv),
			filterProp("account"),
			filterProp("tag"),
			optListProp("display_status",
				"Restrict to these statuses, for example pending, accepted, optimized, rejected."),
			optListProp("available_formats",
				"Restrict to opportunities applicable this way: automated, iac, or manual."),
			optStringProp("search", "Free-text match on id, service or environment."),
			pageProp(),
			pageSizeProp(),
			enumProp(argSortBy, sortByDesc, "monthly_savings",
				"monthly_savings", "annual_savings", "savings_percentage", "recommendation_id", "status"),
			sortOrderProp(),
		),
		calls: []call{{
			path: "/api/v1/providers/{provider}/recommendations",
			query: append(pagingQuery(),
				binding{param: argSortBy, arg: argSortBy, def: "monthly_savings"},
				binding{param: argSortOrder, arg: argSortOrder, def: "desc"},
				binding{param: argService, arg: argService, list: true},
				binding{param: argEnv, arg: argEnv, list: true},
				binding{param: "account", arg: "account", list: true},
				binding{param: "tag", arg: "tag", list: true},
				binding{param: "display_status", arg: "display_status", list: true},
				binding{param: "available_formats", arg: "available_formats", list: true},
				binding{param: "search", arg: "search"},
			),
			rows:  rowsItems,
			paged: true,
			hint: "No recommendations match those filters. Drop a filter, or call with no arguments to see " +
				"the whole backlog before narrowing.",
		}},
	},
	{
		name: "get-recommendation",
		description: lines(
			"**Purpose:** The full case for one savings recommendation.",
			"",
			"**Returns:** financials, affected resources, risk assessment, metrics and the implementation",
			"method. IAM policy and operator artifacts are omitted.",
			"**When to use:** After `list-recommendations`, when the user asks about a specific id.",
			"**NOT for:** Applying anything. This server is read-only; applying happens in the LevelFour",
			"dashboard or with the l4 CLI. Not for scanning the backlog either, use the list tool.",
		),
		schema: object("get_recommendationArguments",
			patternProp("recommendation_id",
				"Recommendation id from list-recommendations, for example REC-1234.",
				`^[A-Za-z][A-Za-z0-9]*-\d+$`),
		),
		calls: []call{{path: "/api/v1/recommendations/{recommendation_id}/details", rows: "resources"}},
		shape: slimRecommendation,
	},
	{
		name: "get-potential-savings",
		description: lines(
			"**Purpose:** Total unbooked opportunity across all recommendations, with per-status counts.",
			"",
			"**Returns:** total monthly and annual potential savings, split by provider and by status.",
			"**When to use:** \"How much could we save in total\", or to size the backlog before listing it.",
			"**NOT for:** Savings already achieved, use `get-realized-savings`. Individual",
			"opportunities, use `list-recommendations`.",
		),
		schema: object("get_potential_savingsArguments"),
		calls: []call{
			{key: "potential_savings", path: "/api/v1/recommendations/potential-savings/summary"},
			{key: "overview", path: "/api/v1/recommendations/overview"},
		},
	},
	{
		name: "get-realized-savings",
		description: lines(
			"**Purpose:** Savings already realized and booked, with who approved them.",
			"",
			"**Returns:** realized totals, ROI and payback, plus a line-item history.",
			"**When to use:** \"How much have we actually saved\", or any question about past results. Each",
			"ledger row carries a full monthly rate, so a sum across rows is a run rate, not a period total.",
			"**NOT for:** Opportunities not yet applied, use `get-potential-savings`. Realized",
			"and potential savings must never be added together.",
		),
		schema: object("get_realized_savingsArguments",
			providerProp(),
			filterProp(argService),
			filterProp(argEnv),
			filterProp("account_id"),
			startProp(argStart),
			endProp(argEnd),
			pageProp(),
			pageSizeProp(),
		),
		calls: []call{
			{key: "summary", path: "/api/v1/recommendations/audit/summary"},
			{
				key:  "breakdown",
				path: "/api/v1/recommendations/audit/breakdown",
				query: append(pagingQuery(),
					binding{param: argProvider, arg: argProvider},
					binding{param: argStart, arg: argStart},
					binding{param: argEnd, arg: argEnd},
					binding{param: argService, arg: argService, list: true},
					binding{param: argEnv, arg: argEnv, list: true},
					binding{param: "account_id", arg: "account_id", list: true},
				),
				rows:  rowsItems,
				paged: true,
				hint: "Nothing has been booked as realized in that window. list-recommendations with " +
					"display_status=[\"optimized\"] shows what has been applied.",
			},
		},
	},
	{
		name: "get-commitment-overview",
		description: lines(
			"**Purpose:** Reserved Instance and Savings Plan position, in aggregate.",
			"",
			"**Returns:** coverage, utilization and the per-service split, plus how many commitments expire soon.",
			"**When to use:** \"How well are we covered.\" For the individual commitments and their expiry dates,",
			"follow with `list-commitments`.",
			"**NOT for:** On-demand spend, use `get-cost-breakdown`.",
		),
		schema: object("get_commitment_overviewArguments", providerProp()),
		calls: []call{
			{key: "overview", path: "/api/v1/commitments/overview", query: []binding{providerQuery()}},
			{key: "by_service", path: "/api/v1/commitments/by-service", query: []binding{providerQuery()}},
		},
	},
	{
		name: "list-commitments",
		description: lines(
			"**Purpose:** The individual Reserved Instances and Savings Plans, with their end dates.",
			"",
			"**Returns:** each commitment with kind, service, monthly commitment amount, start and end date,",
			"and status.",
			"**When to use:** \"What expires before the end of the quarter\", or to size the cliff when a",
			"commitment lapses and its usage reverts to on-demand rates.",
			"**NOT for:** Coverage and utilization percentages, use `get-commitment-overview`.",
		),
		schema: object("list_commitmentsArguments",
			providerProp(),
			optStringProp("kind",
				"Restrict to one commitment kind, for example reserved_instance or savings_plan."),
			optStringProp(argStatus, "Restrict to one status, for example active or expired."),
			pageProp(),
			pageSizeProp(),
		),
		calls: []call{{
			path: "/api/v1/commitments",
			query: append(pagingQuery(),
				binding{param: argProvider, arg: argProvider},
				binding{param: "kind", arg: "kind"},
				binding{param: argStatus, arg: argStatus},
			),
			rows:  rowsItems,
			paged: true,
			hint: "No commitments match. This organization may have none, which get-commitment-overview " +
				"will confirm with a zero coverage figure.",
		}},
	},
	{
		name: "list-anomalies",
		description: lines(
			"**Purpose:** Detected cost anomalies, meaning unexpected spikes.",
			"",
			"**Returns:** anomalies with service, magnitude, detection time and status, plus counts by severity.",
			"**When to use:** \"Did anything spike\", or investigating an unexpected increase in a past month.",
			"Pass start and end to look at a window other than the present.",
			"**NOT for:** Expected or gradual growth, use `get-cost-growth`.",
		),
		schema: object("list_anomaliesArguments",
			providerProp(),
			optEnumProp(argStatus, "Restrict to one anomaly status. Omit for all.",
				"active", "resolved", "dismissed"),
			optEnumProp(argSeverity, "Restrict to one severity. Omit for all.", "critical", "warning"),
			startProp(argStart),
			endProp(argEnd),
			pageProp(),
			pageSizeProp(),
		),
		calls: []call{
			{
				key:  "anomalies",
				path: "/api/v1/anomalies",
				query: append(pagingQuery(),
					binding{param: argProvider, arg: argProvider},
					binding{param: argStatus, arg: argStatus},
					binding{param: argSeverity, arg: argSeverity},
					binding{param: argStart, arg: argStart},
					binding{param: argEnd, arg: argEnd},
				),
				rows:  rowsItems,
				paged: true,
				hint: "No anomalies in that window. Steady growth does not register as an anomaly; " +
					"get-cost-growth is the tool for that.",
			},
			{
				key:   "summary",
				path:  "/api/v1/anomalies/summary",
				query: []binding{{param: argProvider, arg: argProvider}},
			},
		},
	},
}
