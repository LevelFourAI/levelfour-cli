package cli

import (
	"fmt"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

type costsFilterState struct {
	start       string
	end         string
	preset      string
	granularity string
	groupBy     []string

	service     []string
	environment []string
	account     []string
	region      []string
	tagKey      []string
	tagValue    []string

	sortBy     string
	sortByDate string
	sortOrder  string

	page       int
	pageSize   int
	providerID string
	format     string
}

var validPresets = map[string]struct{}{
	"30D": {}, "6M": {}, "12M": {},
}

var validGranularities = map[string]struct{}{
	"daily": {}, "monthly": {},
}

var validSortOrders = map[string]struct{}{
	sortOrderAsc: {}, sortOrderDesc: {},
}

var validGroupByValues = map[string]struct{}{
	"service": {}, "account_id": {}, "region": {}, "tag": {},
}

func (s *costsFilterState) validate() error {
	if s.preset != "" {
		if _, ok := validPresets[s.preset]; !ok {
			return fmt.Errorf("--preset must be one of 30D, 6M, 12M (got %q)", s.preset)
		}
	}
	if s.granularity != "" {
		if _, ok := validGranularities[s.granularity]; !ok {
			return fmt.Errorf("--granularity must be daily or monthly (got %q)", s.granularity)
		}
	}
	if s.sortOrder != "" {
		if _, ok := validSortOrders[s.sortOrder]; !ok {
			return fmt.Errorf("--sort-order must be asc or desc (got %q)", s.sortOrder)
		}
	}
	for _, g := range s.groupBy {
		if _, ok := validGroupByValues[g]; !ok {
			return fmt.Errorf("--group-by must be one of service, account_id, region, tag (got %q)", g)
		}
	}
	if containsString(s.groupBy, "tag") && len(s.tagKey) == 0 {
		return fmt.Errorf("--tag-key is required when --group-by includes tag")
	}
	return nil
}

func containsString(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func buildCostsListRequest(s costsFilterState) *levelfourgo.ListByProviderCostsRequest {
	req := &levelfourgo.ListByProviderCostsRequest{
		Page:     api.IntPtr(s.page),
		PageSize: api.IntPtr(s.pageSize),
	}
	applyCostsListScalarFields(req, s)
	applyCostsListSliceFields(req, s)
	return req
}

func applyCostsListScalarFields(req *levelfourgo.ListByProviderCostsRequest, s costsFilterState) {
	if s.start != "" {
		req.Start = api.StringPtr(s.start)
	}
	if s.end != "" {
		req.End = api.StringPtr(s.end)
	}
	if s.preset != "" {
		req.Preset = api.StringPtr(s.preset)
	}
	if s.granularity != "" {
		req.Granularity = api.StringPtr(s.granularity)
	}
	if s.sortBy != "" {
		req.SortBy = api.StringPtr(s.sortBy)
	}
	if s.sortByDate != "" {
		req.SortByDate = api.StringPtr(s.sortByDate)
	}
	if s.sortOrder != "" {
		so := levelfourgo.ListByProviderCostsRequestSortOrder(s.sortOrder)
		req.SortOrder = &so
	}
	if s.format != "" {
		req.Format = api.StringPtr(s.format)
	}
}

func applyCostsListSliceFields(req *levelfourgo.ListByProviderCostsRequest, s costsFilterState) {
	if len(s.groupBy) > 0 {
		req.GroupBy = s.groupBy
	}
	if len(s.service) > 0 {
		req.Service = s.service
	}
	if len(s.environment) > 0 {
		req.Environment = s.environment
	}
	if len(s.account) > 0 {
		req.AccountID = s.account
	}
	if len(s.region) > 0 {
		req.Region = s.region
	}
	if len(s.tagKey) > 0 {
		req.TagKey = s.tagKey
	}
	if len(s.tagValue) > 0 {
		req.TagValue = s.tagValue
	}
}

func buildCostsRawParams(providerID string, s costsFilterState, format string) map[string][]string {
	params := map[string][]string{}
	params["format"] = []string{format}
	params["page"] = []string{fmt.Sprintf("%d", s.page)}
	params["page_size"] = []string{fmt.Sprintf("%d", s.pageSize)}
	if providerID != "" {
		params["provider_id"] = []string{providerID}
	}
	applyRawParamsScalars(params, s)
	applyRawParamsSlices(params, s)
	return params
}

func applyRawParamsScalars(params map[string][]string, s costsFilterState) {
	scalars := []struct {
		key   string
		value string
	}{
		{"start", s.start},
		{"end", s.end},
		{"preset", s.preset},
		{"granularity", s.granularity},
		{"sort_by", s.sortBy},
		{"sort_by_date", s.sortByDate},
		{"sort_order", s.sortOrder},
	}
	for _, kv := range scalars {
		if kv.value != "" {
			params[kv.key] = []string{kv.value}
		}
	}
}

func applyRawParamsSlices(params map[string][]string, s costsFilterState) {
	slices := []struct {
		key    string
		values []string
	}{
		{"group_by", s.groupBy},
		{"service", s.service},
		{"environment", s.environment},
		{"account_id", s.account},
		{dimRegion, s.region},
		{"tag_key", s.tagKey},
		{"tag_value", s.tagValue},
	}
	for _, kv := range slices {
		if len(kv.values) > 0 {
			params[kv.key] = append(params[kv.key], kv.values...)
		}
	}
}

type costColumn struct {
	header   string
	minWidth int
	value    func(*levelfourgo.ProviderServiceBreakdownItem) string
}

var costColumns = []costColumn{
	{columnService, 0, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetService())
	}},
	{"Cost", 0, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return fmt.Sprintf("$%.2f", item.GetCost())
	}},
	{"Change", 0, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return formatChangePercentage(item.GetChangePercentage())
	}},
	{"Prev Cost", 100, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		p := item.GetPreviousCost()
		if p == nil {
			return dashSymbol
		}
		return fmt.Sprintf("$%.2f", *p)
	}},
	{"Region", 120, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetRegion())
	}},
	{columnAccount, 140, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetAccountID())
	}},
	{"Environment", 160, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetEnvironment())
	}},
	{"Tag Key", 160, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetTagKey())
	}},
	{"Tag Value", 160, func(item *levelfourgo.ProviderServiceBreakdownItem) string {
		return derefOrDash(item.GetTagValue())
	}},
}

func buildCostsBreakdownRows(items []*levelfourgo.ProviderServiceBreakdownItem, width int, groupBy []string) ([]string, [][]string) {
	active := activeCostColumns(width, groupBy)

	headers := make([]string, len(active))
	for i, c := range active {
		headers[i] = c.header
	}

	rows := make([][]string, 0, len(items))
	for _, item := range items {
		row := make([]string, len(active))
		for i, c := range active {
			row[i] = c.value(item)
		}
		rows = append(rows, row)
	}
	return headers, rows
}

func activeCostColumns(width int, groupBy []string) []costColumn {
	promoted := promotedByGroupBy(groupBy)
	var active []costColumn
	for _, c := range costColumns {
		if _, force := promoted[c.header]; force {
			active = append(active, c)
			continue
		}
		if width >= c.minWidth {
			active = append(active, c)
		}
	}
	return active
}

func promotedByGroupBy(groupBy []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, g := range groupBy {
		switch g {
		case "region":
			out["Region"] = struct{}{}
		case "account_id":
			out[columnAccount] = struct{}{}
		case "tag":
			out["Tag Key"] = struct{}{}
			out["Tag Value"] = struct{}{}
		}
	}
	return out
}

func derefOrDash(s *string) string {
	if s == nil || *s == "" {
		return dashSymbol
	}
	return *s
}

func formatChangePercentage(p *float64) string {
	if p == nil {
		return dashSymbol
	}
	v := *p
	sign := ""
	if v > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s%.1f%%", sign, v)
}

func formatRangeLabel(start, end string) string {
	start = strings.TrimSuffix(start, "T00:00:00.000Z")
	end = strings.TrimSuffix(end, "T00:00:00.000Z")
	if start == "" && end == "" {
		return ""
	}
	return fmt.Sprintf("%s → %s", start, end)
}
