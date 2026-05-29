package tuicommon

import (
	"context"
	"errors"
	"testing"

	"charm.land/huh/v2/spinner"
)

func TestRunWithSpinnerNonTTYBypassesSpinner(t *testing.T) {
	orig := IsStdoutTerminal
	IsStdoutTerminal = func() bool { return false }
	defer func() { IsStdoutTerminal = orig }()

	var called bool
	err := RunWithSpinner("test", spinner.ThemeFunc(func(_ bool) *spinner.Styles { return &spinner.Styles{} }), func(_ context.Context) error {
		called = true
		return nil
	})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if !called {
		t.Error("action should have been invoked")
	}
}

func TestRunWithSpinnerPropagatesActionError(t *testing.T) {
	orig := IsStdoutTerminal
	IsStdoutTerminal = func() bool { return false }
	defer func() { IsStdoutTerminal = orig }()

	sentinel := errors.New("boom")
	err := RunWithSpinner("test", spinner.ThemeFunc(func(_ bool) *spinner.Styles { return &spinner.Styles{} }), func(_ context.Context) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

func TestIsStdoutTerminalDefault(t *testing.T) {
	got := IsStdoutTerminal()
	_ = got
}

func TestRunWithSpinnerTTYInvokesSpinner(t *testing.T) {
	origTTY := IsStdoutTerminal
	origSpin := runHuhSpinner
	IsStdoutTerminal = func() bool { return true }
	defer func() {
		IsStdoutTerminal = origTTY
		runHuhSpinner = origSpin
	}()

	var spinnerCalled bool
	runHuhSpinner = func(_ string, _ spinner.Theme, action func(context.Context) error) error {
		spinnerCalled = true
		return action(context.Background())
	}

	var actionCalled bool
	err := RunWithSpinner("tty", spinner.ThemeFunc(func(_ bool) *spinner.Styles { return &spinner.Styles{} }), func(_ context.Context) error {
		actionCalled = true
		return nil
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !spinnerCalled {
		t.Error("expected spinner to be invoked on TTY path")
	}
	if !actionCalled {
		t.Error("expected action to run inside spinner")
	}
}
