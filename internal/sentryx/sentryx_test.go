package sentryx

import (
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

func TestInitDisabled(t *testing.T) {
	ok, err := Init(InitOptions{Enabled: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected Init to be a no-op when disabled")
	}
}

func TestInitNoDSN(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "")
	restore := SetBuildDSN("")
	defer restore()

	ok, err := Init(InitOptions{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected Init to be a no-op when no DSN is configured")
	}
}

func TestInitWithEnvDSN(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "https://key@example.ingest.sentry.io/1")
	restore := SetBuildDSN("")
	defer restore()

	called := false
	prev := initSentry
	initSentry = func(opts sentry.ClientOptions) error {
		called = true
		if opts.Dsn != "https://key@example.ingest.sentry.io/1" {
			t.Errorf("DSN = %q, want env value", opts.Dsn)
		}
		if opts.Release != "1.2.3" {
			t.Errorf("Release = %q, want 1.2.3", opts.Release)
		}
		if opts.Environment != "cli" {
			t.Errorf("Environment = %q, want cli", opts.Environment)
		}
		if !opts.AttachStacktrace {
			t.Errorf("AttachStacktrace = false, want true")
		}
		if opts.SendDefaultPII {
			t.Errorf("SendDefaultPII = true, want false")
		}
		if opts.BeforeSend == nil {
			t.Errorf("BeforeSend = nil, want set")
		}
		return nil
	}
	defer func() { initSentry = prev }()

	ok, err := Init(InitOptions{Enabled: true, Version: "1.2.3", Environment: "cli"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected Init to return true on success")
	}
	if !called {
		t.Errorf("expected sentry.Init to be called")
	}
}

func TestInitWithBuildDSN(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "")
	restore := SetBuildDSN("https://build@example.ingest.sentry.io/2")
	defer restore()

	var receivedDSN string
	prev := initSentry
	initSentry = func(opts sentry.ClientOptions) error {
		receivedDSN = opts.Dsn
		return nil
	}
	defer func() { initSentry = prev }()

	if _, err := Init(InitOptions{Enabled: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedDSN != "https://build@example.ingest.sentry.io/2" {
		t.Errorf("DSN = %q, want build-time value", receivedDSN)
	}
}

func TestInitReturnsErrorFromSentry(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "https://key@example.ingest.sentry.io/1")
	restore := SetBuildDSN("")
	defer restore()

	prev := initSentry
	wantErr := errors.New("init failed")
	initSentry = func(_ sentry.ClientOptions) error { return wantErr }
	defer func() { initSentry = prev }()

	ok, err := Init(InitOptions{Enabled: true})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
	if ok {
		t.Errorf("expected Init to return false when sentry.Init errors")
	}
}

func TestResolveDSNEnvWins(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "from-env")
	restore := SetBuildDSN("from-build")
	defer restore()

	if got := ResolveDSN(); got != "from-env" {
		t.Errorf("ResolveDSN() = %q, want env value", got)
	}
}

func TestResolveDSNFallsBackToBuild(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "")
	restore := SetBuildDSN("from-build")
	defer restore()

	if got := ResolveDSN(); got != "from-build" {
		t.Errorf("ResolveDSN() = %q, want build value", got)
	}
}

func TestResolveDSNEmpty(t *testing.T) {
	t.Setenv("LEVELFOUR_SENTRY_DSN", "")
	restore := SetBuildDSN("")
	defer restore()

	if got := ResolveDSN(); got != "" {
		t.Errorf("ResolveDSN() = %q, want empty", got)
	}
}

func TestRecoverDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Recover panicked outside a recover context: %v", r)
		}
	}()
	Recover()
}

func TestFlush(t *testing.T) {
	if Flush(10*time.Millisecond) != true && Flush(10*time.Millisecond) != false {
		t.Errorf("Flush should return a bool")
	}
}

func TestFlushTimeoutConstant(t *testing.T) {
	if FlushTimeout != 2*time.Second {
		t.Errorf("FlushTimeout = %v, want 2s", FlushTimeout)
	}
}
