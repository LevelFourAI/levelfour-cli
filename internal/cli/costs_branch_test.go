package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

type unknownMsg struct{}

func TestCostsModelUpdatePreReadyUnknownMsg(t *testing.T) {
	m := sampleCostsModel(t)
	updated, cmd := m.Update(unknownMsg{})
	if _, ok := updated.(costsModel); !ok {
		t.Fatal("expected costsModel back")
	}
	if cmd != nil {
		t.Error("unknown msg pre-ready should return nil cmd")
	}
}

func TestCostsModelUpdateReadyUnknownMsg(t *testing.T) {
	m := readyCostsModel(t)
	updated, _ := m.Update(unknownMsg{})
	if _, ok := updated.(costsModel); !ok {
		t.Fatal("expected costsModel back")
	}
}

func TestCostsModelUpdateSpinnerTickLoading(t *testing.T) {
	m := readyCostsModel(t)
	m.loading = true
	updated, _ := m.Update(spinner.TickMsg{Time: time.Now(), ID: 1})
	_ = updated
}

func TestCostsModelUpdateSpinnerTickPreReady(t *testing.T) {
	m := sampleCostsModel(t)
	updated, _ := m.Update(spinner.TickMsg{Time: time.Now(), ID: 1})
	_ = updated
}

func TestRenderSparklineAllZero(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{
		{Value: 0}, {Value: 0},
	}
	got := renderSparkline(pts, 10)
	if len([]rune(got)) != 2 {
		t.Errorf("expected 2 runes for 2 zero points, got %d", len([]rune(got)))
	}
}

func TestBucketValuesSingleBucket(t *testing.T) {
	out := bucketValues([]float64{1, 2, 3, 4}, 1)
	if len(out) != 1 {
		t.Errorf("expected 1 bucket, got %d", len(out))
	}
}

func TestFormatSparklineRangeSinglePoint(t *testing.T) {
	got := formatSparklineRange([]*levelfourgo.SpendingByDateItem{
		{Date: "2026-01-01", Value: 100},
	})
	if got == "" {
		t.Error("single point should render a range string")
	}
}

func TestRenderCostsKPIHeaderNoRange(t *testing.T) {
	m := readyCostsModel(t)
	m.breakdown = &levelfourgo.ProviderServiceBreakdownData{
		ProviderID:      "aws",
		ProviderName:    "AWS",
		TotalPeriodCost: 0,
	}
	got := m.renderCostsKPIHeader()
	if got == "" {
		t.Error("should render header even without range")
	}
}

func TestRenderFilterDrawerNoActiveFilters(t *testing.T) {
	m := readyCostsModel(t)
	m.baseState = costsFilterState{}
	m.mode = tuicommon.ModeFilter
	got := m.renderFilterDrawer()
	if got == "" {
		t.Error("drawer should render even without active filters")
	}
}

func TestUpdateCostsSearchIgnoreUnknown(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeSearch
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	_ = updated
}

func TestBuildTableColumnsEmptyGroup(t *testing.T) {
	m := readyCostsModel(t)
	m.width = 0
	m.baseState.groupBy = nil
	cols := m.buildTableColumns()
	if len(cols) == 0 {
		t.Error("should always have at least the default columns")
	}
}

func TestBuildTableColumnsNarrowWidth(t *testing.T) {
	m := readyCostsModel(t)
	m.width = 30
	cols := m.buildTableColumns()
	for _, c := range cols {
		if c.Width < 5 {
			t.Errorf("column %q width %d < 5 minimum", c.Title, c.Width)
		}
	}
}

func TestListAreaHeightExact(t *testing.T) {
	m := sampleCostsModel(t)
	m.width = 80
	m.height = 7
	if got := m.listAreaHeight(); got != 3 {
		t.Errorf("minimum 3 should clamp, got %d", got)
	}
}

func TestDetailAreaHeightLowerClamp(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 20
	h := m.detailAreaHeight()
	if h < 6 {
		t.Errorf("min clamp at 6, got %d", h)
	}
}

func TestRenderCostsHelpSmallHeight(t *testing.T) {
	m := readyCostsModel(t)
	m.height = 1
	got := m.renderCostsHelp()
	if got == "" {
		t.Error("help should render even at minimal height")
	}
}

func TestRunCostsBreakdownRawNetworkError(t *testing.T) {
	client, _ := api.NewSDKClient("https://127.0.0.1:1", "l4_test_testkey123456789a", "test")
	state := costsFilterState{page: 1, pageSize: 10}
	err := runCostsBreakdownRaw(client, "aws", state, "csv")
	if err == nil {
		t.Error("expected network error")
	}
}

func TestFetchCostsPageNilDataResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"success": true}`))
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
		t.Error("expected unexpected shape error for nil data")
	}
}
