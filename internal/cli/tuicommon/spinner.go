package tuicommon

import (
	"context"
	"os"

	"charm.land/huh/v2/spinner"
	"golang.org/x/term"
)

var IsStdoutTerminal = func() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

var runHuhSpinner = func(title string, theme spinner.Theme, action func(context.Context) error) error {
	s := spinner.New().
		Title(title).
		WithTheme(theme).
		ActionWithErr(action)
	return s.Run()
}

func RunWithSpinner(title string, theme spinner.Theme, action func(context.Context) error) error {
	if !IsStdoutTerminal() {
		return action(context.Background())
	}
	return runHuhSpinner(title, theme, action)
}
