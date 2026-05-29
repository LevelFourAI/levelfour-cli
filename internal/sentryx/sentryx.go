// Package sentryx wraps sentry-go with LevelFour CLI defaults: opt-in gating,
// PII scrubbing, and a build-time-injectable DSN.
package sentryx

import (
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

// sentryDSN is overridden at build time via -ldflags
// "-X github.com/LevelFourAI/levelfour-cli/internal/sentryx.sentryDSN=https://...".
// When empty, the LEVELFOUR_SENTRY_DSN env var is consulted as a fallback.
var sentryDSN = ""

// FlushTimeout is how long Init's caller should give Flush at exit.
const FlushTimeout = 2 * time.Second

// InitOptions configures Sentry initialization.
type InitOptions struct {
	Enabled     bool
	Version     string
	Environment string
}

// initSentry is exposed for test injection.
var initSentry = sentry.Init

// Init starts the Sentry client when enabled and a DSN is available.
// Returns true when the client was actually initialized.
func Init(opts InitOptions) (bool, error) {
	if !opts.Enabled {
		return false, nil
	}
	dsn := ResolveDSN()
	if dsn == "" {
		return false, nil
	}
	err := initSentry(sentry.ClientOptions{
		Dsn:              dsn,
		Release:          opts.Version,
		Environment:      opts.Environment,
		AttachStacktrace: true,
		BeforeSend:       BeforeSend,
		SendDefaultPII:   false,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// ResolveDSN returns the env var override if set, otherwise the build-time DSN.
func ResolveDSN() string {
	if env := os.Getenv("LEVELFOUR_SENTRY_DSN"); env != "" {
		return env
	}
	return sentryDSN
}

// SetBuildDSN is exposed for tests; production callers must inject via ldflags.
func SetBuildDSN(dsn string) func() {
	prev := sentryDSN
	sentryDSN = dsn
	return func() { sentryDSN = prev }
}

// Recover captures any panic and re-panics. Pair with `defer Recover()` at
// process boundaries (main, goroutines).
func Recover() {
	sentry.Recover()
}

// Flush blocks up to timeout while pending events are sent.
func Flush(timeout time.Duration) bool {
	return sentry.Flush(timeout)
}
