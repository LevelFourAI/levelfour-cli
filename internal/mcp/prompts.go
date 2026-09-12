package mcp

import "strings"

// Port of the hosted server's own prompts. A FinOps question is
// rarely one tool call, and three of the steps below correct mistakes a model
// makes reliably: comparing against a partial month without saying so, reporting
// a tag total without its allocation coverage, and adding realized savings to
// potential savings. Each prompt returns instructions, never results, so the
// client's model still makes every call.

type promptDef struct {
	name        string
	description string
	arg         string
	argDesc     string
	fallback    string // stands in for the argument when the caller omits it
	body        func(value string) string
}

var prompts = []promptDef{
	{
		name:        "monthly_bill_review",
		description: "Review a month's cloud spend: what changed, what drove it, what to do",
		arg:         "month",
		argDesc:     "Month to review, YYYY-MM. Omit for the most recently completed month.",
		fallback:    "the most recently completed month",
		body:        monthlyBillReview,
	},
	{
		name:        "quarterly_commitment_review",
		description: "Assess commitment coverage and find expiry cliffs before they hit the bill",
		arg:         "quarter_end",
		argDesc:     "End of the period to look at, YYYY-MM-DD.",
		fallback:    "the end of the current quarter",
		body:        quarterlyCommitmentReview,
	},
	{
		name:        "waste_by_team",
		description: "Attribute spend and savings opportunity by team, with the missing join stated",
		arg:         "tag_key",
		argDesc:     "Cost allocation tag key that identifies a team, for example 'team' or 'owner'.",
		fallback:    "the tag key that identifies teams",
		body:        wasteByTeam,
	},
}

func monthlyBillReview(target string) string {
	return lines(
		"Review cloud spend for "+target+" and produce a short written summary.",
		"",
		"Work in this order and do not skip steps.",
		"",
		"1. `get-cost-summary` for the totals and the month-over-month direction.",
		"2. `get-cost-growth` to find what grew. Leave `compare_to` at its default.",
		"   Only use `previous_month` if the user asked about calendar months specifically, and if",
		"   you do, state that the current month is partial and say how many days each window covers.",
		"3. If something grew materially, `get-cost-series` over that month plus the one",
		"   before it, to find the day the change started. A step on one day and a gradual ramp have",
		"   different causes and different fixes.",
		"4. `get-cost-breakdown` over the same window, grouped to match what grew, to",
		"   attribute it to a service and an account.",
		"5. `get-usage-costs` for the service that grew, to separate a price change from a",
		"   volume change. Use the quantity column, not the cost column, which rounds to cents.",
		"6. `list-anomalies` scoped to that month with `start` and `end`, to check whether",
		"   any of it was already flagged as a spike.",
		"7. `list-recommendations` filtered to the service and account that grew, to say",
		"   what can actually be done. Do not list the whole backlog.",
		"",
		"When you write it up: give the number, the date it started, the account and service, whether",
		"it was price or volume, and the single largest addressable opportunity with its id. Say",
		"plainly which parts you could not determine rather than filling them in.",
	)
}

func quarterlyCommitmentReview(horizon string) string {
	return lines(
		"Assess this organization's Reserved Instance and Savings Plan position through "+horizon+".",
		"",
		"1. `get-commitment-overview` for coverage and utilization. Low coverage and low",
		"   utilization are opposite problems: one means paying on-demand unnecessarily, the other",
		"   means paying for capacity nobody used.",
		"2. `list-commitments` for the individual commitments and their end dates. This is",
		"   the only place the dates live. Identify everything expiring before "+horizon+".",
		"3. For each expiring commitment, size the cliff: when it lapses, that usage reverts to",
		"   on-demand rates. Use its monthly commitment amount as the floor of the exposure.",
		"4. `get-cost-forecast` with the 90 day horizon for where spend is heading, and say",
		"   explicitly that the forecast does not know about the expiries you just found.",
		"5. `list-recommendations` with `service` set to whatever the expiring commitments",
		"   cover, in case a rightsizing should happen before anything is renewed at the current size.",
		"",
		"LevelFour has no budget object. If the user asks whether they will exceed a budget, ask them",
		"for the figure rather than inventing a baseline from past spend.",
	)
}

func wasteByTeam(key string) string {
	return lines(
		"Attribute cloud spend and savings opportunity by team, using "+key+".",
		"",
		"1. `get-costs-by-tag` with no arguments to see which tag keys exist. Pick the one that",
		"   identifies teams. If none does, stop and tell the user that cost allocation tags are not",
		"   set up, because everything below depends on them.",
		"2. `get-costs-by-tag` with that key for spend per team. Read the allocation coverage in",
		"   the same response and report it. If a large share of spend carries no tag, every per-team",
		"   number is a share of the tagged remainder, and saying so is the difference between a",
		"   useful answer and a misleading one.",
		"3. `list-recommendations` for the savings backlog.",
		"",
		"At step 3 you hit a real limit: recommendations carry a service, an environment and an",
		"account, but not a team tag. There is no join from an opportunity to a team. Do not invent",
		"one. Two honest options, and say which you took:",
		"",
		"  - Group opportunities by `account` and, if the accounts map to teams, present it that way",
		"    with the mapping stated. `list-accounts` gives the account names.",
		"  - Group by `environment` and present it as an environment view rather than a team view.",
		"",
		"Report the teams by spend, the opportunities by account or environment, and state plainly",
		"that the two are grouped differently.",
	)
}

func (p promptDef) render(args map[string]string) string {
	value := strings.TrimSpace(args[p.arg])
	if value == "" {
		value = p.fallback
	}
	return p.body(value)
}
