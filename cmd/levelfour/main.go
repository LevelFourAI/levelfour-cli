package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/LevelFourAI/levelfour-cli/internal/cli"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/LevelFourAI/levelfour-cli/internal/sentryx"
	"github.com/getsentry/sentry-go"
)

func main() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT)
	go func() {
		<-sig
		signal.Stop(sig)
		fmt.Fprint(os.Stderr, "\r\033[2K")
		os.Exit(cli.ExitSIGINT)
	}()

	code := run()
	sentryx.Flush(sentryx.FlushTimeout)
	os.Exit(code)
}

func run() (exitCode int) {
	defer func() {
		if r := recover(); r != nil {
			sentry.CurrentHub().Recover(r)
			exitCode = cli.ExitError
		}
	}()

	if err := cli.Execute(); err != nil {
		if errors.Is(err, cli.ErrIssuesFound) {
			return cli.ExitIssuesFound
		}
		// An error can carry a file name or an API message, so it is printed
		// without control characters.
		msg := output.SanitizeMultiline(err.Error())
		fmt.Fprintln(os.Stderr, msg)
		if strings.Contains(msg, "not authenticated") {
			return cli.ExitAuthRequired
		}
		return cli.ExitError
	}
	return 0
}
