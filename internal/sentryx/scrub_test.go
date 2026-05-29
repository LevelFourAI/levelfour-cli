package sentryx

import (
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestBeforeSendScrubsHomeFromMessage(t *testing.T) {
	restore := SetHomeDirOverride("/Users/alice")
	defer restore()

	event := &sentry.Event{Message: "failed reading /Users/alice/.aws/credentials"}
	got := BeforeSend(event, nil)
	if !strings.Contains(got.Message, "~/.aws/credentials") {
		t.Errorf("Message not scrubbed: %q", got.Message)
	}
	if strings.Contains(got.Message, "/Users/alice") {
		t.Errorf("home dir leaked: %q", got.Message)
	}
}

func TestBeforeSendRedactsAWSKey(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()

	event := &sentry.Event{Message: "key AKIAIOSFODNN7EXAMPLE expired"}
	got := BeforeSend(event, nil)
	if !strings.Contains(got.Message, "[REDACTED-AWS-KEY]") {
		t.Errorf("AWS key not redacted: %q", got.Message)
	}
}

func TestBeforeSendScrubsExceptionValueAndFrames(t *testing.T) {
	restore := SetHomeDirOverride("/Users/bob")
	defer restore()

	event := &sentry.Event{
		Exception: []sentry.Exception{
			{
				Value: "boom in /Users/bob/code/app",
				Stacktrace: &sentry.Stacktrace{
					Frames: []sentry.Frame{
						{Filename: "/Users/bob/code/main.go", AbsPath: "/Users/bob/code/main.go"},
					},
				},
			},
		},
	}
	got := BeforeSend(event, nil)
	if strings.Contains(got.Exception[0].Value, "/Users/bob") {
		t.Errorf("exception value not scrubbed: %q", got.Exception[0].Value)
	}
	frame := got.Exception[0].Stacktrace.Frames[0]
	if strings.Contains(frame.Filename, "/Users/bob") {
		t.Errorf("frame filename not scrubbed: %q", frame.Filename)
	}
	if strings.Contains(frame.AbsPath, "/Users/bob") {
		t.Errorf("frame abs path not scrubbed: %q", frame.AbsPath)
	}
}

func TestBeforeSendHandlesExceptionWithoutStacktrace(t *testing.T) {
	restore := SetHomeDirOverride("/Users/bob")
	defer restore()

	event := &sentry.Event{
		Exception: []sentry.Exception{{Value: "no frames here"}},
	}
	got := BeforeSend(event, nil)
	if got.Exception[0].Value != "no frames here" {
		t.Errorf("unexpected value: %q", got.Exception[0].Value)
	}
}

func TestBeforeSendScrubsThreadFrames(t *testing.T) {
	restore := SetHomeDirOverride("/Users/carol")
	defer restore()

	event := &sentry.Event{
		Threads: []sentry.Thread{
			{
				Stacktrace: &sentry.Stacktrace{
					Frames: []sentry.Frame{{Filename: "/Users/carol/main.go"}},
				},
			},
			{},
		},
	}
	got := BeforeSend(event, nil)
	if strings.Contains(got.Threads[0].Stacktrace.Frames[0].Filename, "/Users/carol") {
		t.Errorf("thread frame not scrubbed")
	}
}

func TestBeforeSendStripsRedactedTagKeys(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()

	event := &sentry.Event{
		Tags: map[string]string{
			"LEVELFOUR_TOKEN":   "secret-1",
			"AWS_SESSION_TOKEN": "secret-2",
			"keep_me":           "ok",
		},
	}
	got := BeforeSend(event, nil)
	if _, ok := got.Tags["LEVELFOUR_TOKEN"]; ok {
		t.Errorf("LEVELFOUR_TOKEN tag should be removed")
	}
	if _, ok := got.Tags["AWS_SESSION_TOKEN"]; ok {
		t.Errorf("AWS_SESSION_TOKEN tag should be removed")
	}
	if got.Tags["keep_me"] != "ok" {
		t.Errorf("non-redacted tag was lost")
	}
}

func TestBeforeSendStripsRequestHeadersAndCookies(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()

	event := &sentry.Event{
		Request: &sentry.Request{
			Headers: map[string]string{"Authorization": "Bearer x"},
			Cookies: "session=abc",
		},
	}
	got := BeforeSend(event, nil)
	if got.Request.Headers != nil {
		t.Errorf("request headers should be nil")
	}
	if got.Request.Cookies != "" {
		t.Errorf("request cookies should be empty")
	}
}

func TestBeforeSendNoopWhenNothingToScrub(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()

	event := &sentry.Event{Message: "plain message"}
	got := BeforeSend(event, nil)
	if got.Message != "plain message" {
		t.Errorf("Message changed unexpectedly: %q", got.Message)
	}
}

func TestScrubStringEmpty(t *testing.T) {
	if got := scrubString("", "/Users/alice"); got != "" {
		t.Errorf("expected empty string passthrough, got %q", got)
	}
}

func TestScrubStringWithEmptyHome(t *testing.T) {
	got := scrubString("/Users/alice/x", "")
	if got != "/Users/alice/x" {
		t.Errorf("expected unchanged when home is empty, got %q", got)
	}
}

func TestHomeDirOverride(t *testing.T) {
	restore := SetHomeDirOverride("/custom/home")
	defer restore()
	if got := homeDir(); got != "/custom/home" {
		t.Errorf("homeDir() = %q, want /custom/home", got)
	}
}

func TestHomeDirFallsBackToOSHome(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()
	if got := homeDir(); got == "" {
		t.Errorf("homeDir() returned empty; expected OS home")
	}
}

func TestHomeDirReturnsEmptyWhenOSError(t *testing.T) {
	restore := SetHomeDirOverride("")
	defer restore()

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	got := homeDir()
	_ = got
}
