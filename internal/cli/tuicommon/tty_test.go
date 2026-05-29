package tuicommon

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestFilterTerminalNoiseStripsOSCResponse(t *testing.T) {
	noise := tea.KeyPressMsg{Text: "11;rgb:1919/2020/1919"}
	if got := FilterTerminalNoise(nil, noise); got != nil {
		t.Errorf("expected OSC noise to be stripped, got %v", got)
	}
}

func TestFilterTerminalNoisePassesRegularKeys(t *testing.T) {
	k := tea.KeyPressMsg{Code: 'q', Text: "q"}
	if got := FilterTerminalNoise(nil, k); got == nil {
		t.Error("expected regular key to pass through")
	}
}

func TestFilterTerminalNoisePassesNonKeyMessages(t *testing.T) {
	msg := tea.WindowSizeMsg{Width: 80, Height: 24}
	got := FilterTerminalNoise(nil, msg)
	if _, ok := got.(tea.WindowSizeMsg); !ok {
		t.Errorf("expected WindowSizeMsg to pass through, got %T", got)
	}
}

func TestModeConstants(t *testing.T) {
	if ModeNormal == ModeSearch {
		t.Error("mode constants must be distinct")
	}
	if ModeFilter == ModeHelp {
		t.Error("ModeFilter must be distinct from ModeHelp")
	}
}
