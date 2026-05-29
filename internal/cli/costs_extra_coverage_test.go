package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestCountItemsFallback(t *testing.T) {
	svc := "EC2"
	data := &levelfourgo.ProviderServiceBreakdownData{
		Items: []*levelfourgo.ProviderServiceBreakdownItem{
			{Service: &svc, Cost: 1.0},
			{Service: &svc, Cost: 2.0},
		},
	}
	if got := countItems(data); got != 2 {
		t.Errorf("expected fallback to len(items)=2, got %d", got)
	}
}

func TestCountItemsPagination(t *testing.T) {
	data := &levelfourgo.ProviderServiceBreakdownData{
		Pagination: &levelfourgo.PaginationMeta{TotalItems: 99},
	}
	if got := countItems(data); got != 99 {
		t.Errorf("expected pagination total 99, got %d", got)
	}
}

func TestRenderCostsBreakdownKPIsNil(t *testing.T) {
	renderCostsBreakdownKPIs(nil)
}

func TestRenderCostsBreakdownKPIsEmpty(t *testing.T) {
	renderCostsBreakdownKPIs(&levelfourgo.ProviderServiceBreakdownData{})
}

func TestRenderCostsBreakdownKPIsWithRange(t *testing.T) {
	data := &levelfourgo.ProviderServiceBreakdownData{
		TotalPeriodCost: 100,
		StartDate:       "2026-01-01",
		EndDate:         "2026-01-31",
		ProviderName:    "AWS",
	}
	renderCostsBreakdownKPIs(data)
}

func TestRunCostsBreakdownRawError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("boom"))
	}))
	defer srv.Close()

	client, err := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "test")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	state := costsFilterState{page: 1, pageSize: 10}
	err = runCostsBreakdownRaw(client, "aws", state, "csv")
	if err == nil {
		t.Error("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected 500 in error, got %v", err)
	}
}

func TestBuildCostsRawParamsAllFields(t *testing.T) {
	state := costsFilterState{
		start:       "2026-01-01",
		end:         "2026-01-31",
		preset:      "30D",
		granularity: "daily",
		groupBy:     []string{"service"},
		service:     []string{"EC2"},
		environment: []string{"prod"},
		account:     []string{"111"},
		region:      []string{"us-east-1"},
		tagKey:      []string{"Env"},
		tagValue:    []string{"prod"},
		sortBy:      "cost",
		sortByDate:  "2026-01-15",
		sortOrder:   "desc",
		page:        1,
		pageSize:    20,
	}
	params := buildCostsRawParams("aws", state, "csv")
	for _, key := range []string{
		"format", "provider_id", "start", "end", "preset",
		"granularity", "sort_by", "sort_by_date", "sort_order",
		"group_by", "service", "environment", "account_id",
		"region", "tag_key", "tag_value",
	} {
		if _, ok := params[key]; !ok {
			t.Errorf("param %q missing", key)
		}
	}
}

func TestBuildCostsRawParamsEmpty(t *testing.T) {
	params := buildCostsRawParams("", costsFilterState{page: 1, pageSize: 10}, "csv")
	if _, ok := params["provider_id"]; ok {
		t.Error("empty providerID should not set provider_id")
	}
}

func TestSampleValuesShortList(t *testing.T) {
	got := sampleValues([]string{"a", "b"})
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") {
		t.Errorf("short list should include all values: %q", got)
	}
	if strings.Contains(got, "more") {
		t.Errorf("short list should not say 'more': %q", got)
	}
}

func TestSampleValuesLongList(t *testing.T) {
	got := sampleValues([]string{"a", "b", "c", "d", "e"})
	if !strings.Contains(got, "more") {
		t.Errorf("long list should show '... more': %q", got)
	}
}

func TestSampleValuesEmpty(t *testing.T) {
	if got := sampleValues(nil); got != "—" {
		t.Errorf("empty list should render em-dash, got %q", got)
	}
}

func TestRenderFilterDimensionEmpty(t *testing.T) {
	empty := &levelfourgo.ProviderFilterOptionsData{}
	if err := renderFilterDimension("service", empty); err != nil {
		t.Errorf("empty dimension should not error, got %v", err)
	}
}

func TestRenderFilterDimensionAllSupported(t *testing.T) {
	data := &levelfourgo.ProviderFilterOptionsData{
		Services:  []string{"EC2"},
		Regions:   []string{"us-east-1"},
		Accounts:  []string{"111"},
		TagKeys:   []string{"Env"},
		TagValues: []string{"prod"},
	}
	for _, dim := range []string{"service", "services", "region", "account_id", "tag-key", "tag_values", "tag_value"} {
		if err := renderFilterDimension(dim, data); err != nil {
			t.Errorf("dim %q should not error, got %v", dim, err)
		}
	}
}

func TestRenderFilterDimensionUnknown(t *testing.T) {
	data := &levelfourgo.ProviderFilterOptionsData{}
	if err := renderFilterDimension("bogus", data); err == nil {
		t.Error("unknown dim should error")
	}
}

func TestFetchCostsPageError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "test")
	m := readyCostsModel(t)
	m.client = client
	cmd := m.fetchCostsPage()
	msg := cmd()
	fetched, ok := msg.(costsListFetchMsg)
	if !ok {
		t.Fatalf("expected costsListFetchMsg, got %T", msg)
	}
	if fetched.err == nil {
		t.Error("expected error from 500 response")
	}
}

func TestFetchCostsPageSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"data":{"provider_id":"aws","provider_name":"AWS","period":"2026-04","start_date":"2026-04-01","end_date":"2026-04-30","total_period_cost":100.0,"items":[{"service":"EC2","cost":50.0}],"pagination":{"current_page":1,"total_pages":1,"total_items":1,"page_size":20,"has_next":false,"has_previous":false}}}`))
	}))
	defer srv.Close()

	client, _ := api.NewSDKClient(srv.URL, "l4_test_testkey123456789a", "test")
	m := readyCostsModel(t)
	m.client = client
	cmd := m.fetchCostsPage()
	msg := cmd()
	fetched, ok := msg.(costsListFetchMsg)
	if !ok {
		t.Fatalf("expected costsListFetchMsg, got %T", msg)
	}
	if fetched.err != nil {
		t.Errorf("expected no error, got %v", fetched.err)
	}
	if len(fetched.items) != 1 {
		t.Errorf("expected 1 item, got %d", len(fetched.items))
	}
}

func TestListAreaHeightWithDetailOpen(t *testing.T) {
	m := sampleCostsModel(t)
	m.width = 120
	m.height = 50
	m.detailOpen = true
	if got := m.listAreaHeight(); got <= 0 {
		t.Errorf("list area with detail open should be positive, got %d", got)
	}
}

func TestDetailAreaHeightUpperClamp(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 200
	h := m.detailAreaHeight()
	if h > 16 {
		t.Errorf("detail height should clamp to 16, got %d", h)
	}
}

func TestRenderSparklineNegativeWidth(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{{Value: 1.0}, {Value: 2.0}}
	got := renderSparkline(pts, 0)
	if got == "" {
		t.Error("should render at least 1 bucket for non-empty input")
	}
}

func TestBucketValuesEqualLength(t *testing.T) {
	out := bucketValues([]float64{1, 2, 3}, 3)
	if len(out) != 3 {
		t.Errorf("expected 3 values, got %d", len(out))
	}
}

func TestFormatSparklineRangeMinMax(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{
		{Date: "2026-01-01", Value: 10},
		{Date: "2026-01-02", Value: 50},
		{Date: "2026-01-03", Value: 20},
	}
	got := formatSparklineRange(pts)
	if !strings.Contains(got, "min $10") {
		t.Errorf("should show min, got %q", got)
	}
	if !strings.Contains(got, "max $50") {
		t.Errorf("should show max, got %q", got)
	}
}

func TestHandleCostsSortOrderToggle(t *testing.T) {
	m := readyCostsModel(t)
	m.sortOrder = "asc"
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S", Mod: tea.ModShift})
	m = updated.(costsModel)
	if m.sortOrder != sortOrderDesc {
		t.Errorf("S with asc should toggle to desc, got %q", m.sortOrder)
	}
	if cmd == nil {
		t.Error("sort toggle should produce refetch command")
	}
}

func TestRefreshDetailFromSelectedClosed(t *testing.T) {
	m := readyCostsModel(t)
	m.detailOpen = false
	m.refreshDetailFromSelected()
}

func TestRefreshDetailFromSelectedOutOfBounds(t *testing.T) {
	m := readyCostsModel(t)
	m.detailOpen = true
	m.items = nil
	m.refreshDetailFromSelected()
}

func TestNewCostsModelDefaultSortOrder(t *testing.T) {
	state := costsFilterState{sortOrder: "asc"}
	data := &levelfourgo.ProviderServiceBreakdownData{ProviderID: "aws"}
	m := newCostsModel(nil, "aws", data, nil, nil, 20, state)
	if m.sortOrder != "asc" {
		t.Errorf("should honor state.sortOrder, got %q", m.sortOrder)
	}
}

func TestNewCostsModelWithPagination(t *testing.T) {
	data := &levelfourgo.ProviderServiceBreakdownData{ProviderID: "aws"}
	p := &levelfourgo.PaginationMeta{CurrentPage: 5}
	m := newCostsModel(nil, "aws", data, nil, p, 20, costsFilterState{})
	if m.page != 5 {
		t.Errorf("should read currentPage from pagination, got %d", m.page)
	}
}
