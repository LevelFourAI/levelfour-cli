package cli

import (
	"strings"
	"testing"

	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestCostsFilterStateValidate(t *testing.T) {
	tests := []struct {
		name    string
		state   costsFilterState
		wantErr string
	}{
		{"empty is valid", costsFilterState{}, ""},
		{"valid preset", costsFilterState{preset: "30D"}, ""},
		{"invalid preset", costsFilterState{preset: "99D"}, "--preset"},
		{"valid granularity daily", costsFilterState{granularity: "daily"}, ""},
		{"valid granularity monthly", costsFilterState{granularity: "monthly"}, ""},
		{"invalid granularity", costsFilterState{granularity: "weekly"}, "--granularity"},
		{"valid sort-order asc", costsFilterState{sortOrder: "asc"}, ""},
		{"valid sort-order desc", costsFilterState{sortOrder: "desc"}, ""},
		{"invalid sort-order", costsFilterState{sortOrder: "up"}, "--sort-order"},
		{"valid group-by service", costsFilterState{groupBy: []string{"service"}}, ""},
		{"valid group-by multi", costsFilterState{groupBy: []string{"service", "region"}}, ""},
		{"invalid group-by", costsFilterState{groupBy: []string{"provider"}}, "--group-by"},
		{"group-by tag needs tag-key", costsFilterState{groupBy: []string{"tag"}}, "--tag-key"},
		{"group-by tag with tag-key is valid", costsFilterState{groupBy: []string{"tag"}, tagKey: []string{"Environment"}}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}

func TestBuildCostsListRequest(t *testing.T) {
	state := costsFilterState{
		start:       "2026-01-01",
		end:         "2026-01-31",
		preset:      "30D",
		granularity: "monthly",
		groupBy:     []string{"service", "region"},
		service:     []string{"EC2", "RDS"},
		environment: []string{"prod"},
		account:     []string{"111", "222"},
		region:      []string{"us-east-1"},
		tagKey:      []string{"Environment"},
		tagValue:    []string{"prod"},
		sortBy:      "cost",
		sortByDate:  "2026-01-15",
		sortOrder:   "desc",
		page:        2,
		pageSize:    50,
		format:      "table",
	}

	req := buildCostsListRequest(state)

	if req.Start == nil || *req.Start != "2026-01-01" {
		t.Errorf("Start not set correctly: %v", req.Start)
	}
	if req.End == nil || *req.End != "2026-01-31" {
		t.Errorf("End not set correctly: %v", req.End)
	}
	if req.Preset == nil || *req.Preset != "30D" {
		t.Errorf("Preset not set correctly: %v", req.Preset)
	}
	if req.Granularity == nil || *req.Granularity != "monthly" {
		t.Errorf("Granularity not set correctly: %v", req.Granularity)
	}
	if len(req.GroupBy) != 2 || req.GroupBy[0] != "service" || req.GroupBy[1] != "region" {
		t.Errorf("GroupBy not set correctly: %v", req.GroupBy)
	}
	if len(req.Service) != 2 {
		t.Errorf("Service not set correctly: %v", req.Service)
	}
	if len(req.AccountID) != 2 {
		t.Errorf("AccountID not set correctly: %v", req.AccountID)
	}
	if req.SortByDate == nil || *req.SortByDate != "2026-01-15" {
		t.Errorf("SortByDate not set correctly: %v", req.SortByDate)
	}
	if req.SortOrder == nil || string(*req.SortOrder) != "desc" {
		t.Errorf("SortOrder not set correctly: %v", req.SortOrder)
	}
	if req.Page == nil || *req.Page != 2 {
		t.Errorf("Page not set correctly: %v", req.Page)
	}
}

func TestBuildCostsListRequestEmpty(t *testing.T) {
	state := costsFilterState{page: 1, pageSize: 20}
	req := buildCostsListRequest(state)

	if req.Start != nil {
		t.Errorf("Start should be nil")
	}
	if req.Preset != nil {
		t.Errorf("Preset should be nil")
	}
	if req.GroupBy != nil {
		t.Errorf("GroupBy should be nil")
	}
	if req.Page == nil || *req.Page != 1 {
		t.Errorf("Page should be 1, got %v", req.Page)
	}
}

func TestBuildCostsRawParams(t *testing.T) {
	state := costsFilterState{
		start:     "2026-01-01",
		end:       "2026-01-31",
		groupBy:   []string{"service", "region"},
		service:   []string{"EC2"},
		account:   []string{"111"},
		tagKey:    []string{"Env"},
		page:      1,
		pageSize:  20,
		sortOrder: "desc",
	}

	params := buildCostsRawParams("aws", state, "csv")

	if got := params["format"]; len(got) != 1 || got[0] != "csv" {
		t.Errorf("format param wrong: %v", got)
	}
	if got := params["provider_id"]; len(got) != 1 || got[0] != "aws" {
		t.Errorf("provider_id param wrong: %v", got)
	}
	if got := params["group_by"]; len(got) != 2 {
		t.Errorf("group_by should have 2 values: %v", got)
	}
	if got := params["account_id"]; len(got) != 1 || got[0] != "111" {
		t.Errorf("account_id param wrong: %v", got)
	}
	if got := params["service"]; len(got) != 1 || got[0] != "EC2" {
		t.Errorf("service param wrong: %v", got)
	}
}

func TestBuildCostsBreakdownRows(t *testing.T) {
	svc := "EC2"
	region := "us-east-1"
	prev := 450.0
	change := 5.5

	items := []*levelfourgo.ProviderServiceBreakdownItem{
		{
			Service:          &svc,
			Region:           &region,
			Cost:             500.0,
			PreviousCost:     &prev,
			ChangePercentage: &change,
		},
		{
			Service: &svc,
			Cost:    100.0,
		},
	}

	t.Run("narrow terminal hides wide columns", func(t *testing.T) {
		headers, rows := buildCostsBreakdownRows(items, 60, nil)
		if len(headers) != 3 {
			t.Errorf("expected 3 columns at width 60, got %d: %v", len(headers), headers)
		}
		if len(rows) != 2 {
			t.Errorf("expected 2 rows, got %d", len(rows))
		}
	})

	t.Run("wide terminal shows all columns", func(t *testing.T) {
		headers, _ := buildCostsBreakdownRows(items, 200, nil)
		if len(headers) < 7 {
			t.Errorf("expected at least 7 columns at width 200, got %d: %v", len(headers), headers)
		}
	})

	t.Run("group-by promotes columns even on narrow terminals", func(t *testing.T) {
		headers, _ := buildCostsBreakdownRows(items, 60, []string{"account_id", "region"})
		hasAccount := false
		hasRegion := false
		for _, h := range headers {
			if h == "Account" {
				hasAccount = true
			}
			if h == "Region" {
				hasRegion = true
			}
		}
		if !hasAccount {
			t.Errorf("Account column should be promoted: %v", headers)
		}
		if !hasRegion {
			t.Errorf("Region column should be promoted: %v", headers)
		}
	})

	t.Run("nil service renders as dash", func(t *testing.T) {
		nilItem := []*levelfourgo.ProviderServiceBreakdownItem{{Cost: 10.0}}
		_, rows := buildCostsBreakdownRows(nilItem, 200, nil)
		if len(rows) != 1 {
			t.Fatalf("expected 1 row")
		}
		if rows[0][0] != "—" {
			t.Errorf("expected service to be em-dash for nil, got %q", rows[0][0])
		}
	})
}

func TestFormatChangePercentage(t *testing.T) {
	pos := 5.2
	neg := -2.1
	zero := 0.0

	tests := []struct {
		name string
		in   *float64
		want string
	}{
		{"nil", nil, "—"},
		{"positive", &pos, "+5.2%"},
		{"negative", &neg, "-2.1%"},
		{"zero", &zero, "0.0%"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatChangePercentage(tt.in)
			if got != tt.want {
				t.Errorf("formatChangePercentage(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDerefOrDash(t *testing.T) {
	s := "hello"
	empty := ""

	tests := []struct {
		name string
		in   *string
		want string
	}{
		{"nil", nil, "—"},
		{"empty", &empty, "—"},
		{"non-empty", &s, "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derefOrDash(tt.in)
			if got != tt.want {
				t.Errorf("derefOrDash(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatRangeLabel(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
		want  string
	}{
		{"both empty", "", "", ""},
		{"plain dates", "2026-01-01", "2026-01-31", "2026-01-01 → 2026-01-31"},
		{"iso with time suffix trimmed", "2026-01-01T00:00:00.000Z", "2026-01-31T00:00:00.000Z", "2026-01-01 → 2026-01-31"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatRangeLabel(tt.start, tt.end)
			if got != tt.want {
				t.Errorf("formatRangeLabel = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPromotedByGroupBy(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		wantKey string
	}{
		{"region promotes Region", []string{"region"}, "Region"},
		{"account_id promotes Account", []string{"account_id"}, "Account"},
		{"tag promotes Tag Key", []string{"tag"}, "Tag Key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := promotedByGroupBy(tt.in)
			if _, ok := got[tt.wantKey]; !ok {
				t.Errorf("expected %q in promoted set, got %v", tt.wantKey, got)
			}
		})
	}
}
