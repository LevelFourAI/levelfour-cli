package cli

import (
	"testing"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/LevelFourAI/levelfour-cli/internal/cli/tuicommon"
	levelfourgo "github.com/LevelFourAI/levelfour-go"
)

func TestCostsModelSpinnerTickWhenReady(t *testing.T) {
	m := readyCostsModel(t)
	m.loading = false
	updated, cmd := m.Update(spinner.TickMsg{Time: time.Now(), ID: 1})
	if _, ok := updated.(costsModel); !ok {
		t.Fatal("expected costsModel")
	}
	if cmd != nil {
		t.Error("tick while ready+not loading should return nil cmd")
	}
}

func TestUpdateCostsSearchConfirmDirect(t *testing.T) {
	m := readyCostsModel(t)
	m.mode = tuicommon.ModeSearch
	m.searchInput.SetValue("RDS")
	updated, _ := m.updateCostsSearch(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(costsModel)
	if m.mode != tuicommon.ModeNormal {
		t.Errorf("confirm should leave search mode, got %d", m.mode)
	}
}

func TestBuildTableColumnsZeroColumns(t *testing.T) {
	m := readyCostsModel(t)
	orig := costColumns
	defer func() { costColumns = orig }()
	costColumns = nil
	cols := m.buildTableColumns()
	if len(cols) != 0 {
		t.Errorf("expected 0 cols for empty costColumns, got %d", len(cols))
	}
}

func TestDetailAreaHeightBelowMin(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 22
	h := m.detailAreaHeight()
	if h < 6 {
		t.Errorf("should clamp to min 6, got %d", h)
	}
}

func TestRenderSparklineMixedPositiveNegative(t *testing.T) {
	pts := []*levelfourgo.SpendingByDateItem{
		{Value: -10}, {Value: 5}, {Value: -3},
	}
	got := renderSparkline(pts, 3)
	if len([]rune(got)) != 3 {
		t.Errorf("expected 3 runes, got %d", len([]rune(got)))
	}
}

func TestDetailAreaHeightMidClampRange(t *testing.T) {
	m := sampleCostsModel(t)
	m.height = 18
	h := m.detailAreaHeight()
	if h < 6 {
		t.Errorf("min clamp at 6 reached, got %d", h)
	}
}
