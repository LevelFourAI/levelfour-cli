package sentryx

import (
	"os"
	"regexp"
	"strings"

	"github.com/getsentry/sentry-go"
)

var awsAccessKeyRe = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

// redactedTagKeys are stripped from event.Tags before transport.
var redactedTagKeys = []string{
	"LEVELFOUR_TOKEN",
	"LEVELFOUR_API_KEY",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
}

// BeforeSend scrubs PII from outgoing events: replaces $HOME with "~" in
// strings and stack frames, redacts AWS access keys, and drops sensitive
// values from tags and request headers.
func BeforeSend(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	home := homeDir()

	event.Message = scrubString(event.Message, home)

	for i := range event.Exception {
		exc := &event.Exception[i]
		exc.Value = scrubString(exc.Value, home)
		if exc.Stacktrace != nil {
			scrubFrames(exc.Stacktrace.Frames, home)
		}
	}

	for i := range event.Threads {
		th := &event.Threads[i]
		if th.Stacktrace != nil {
			scrubFrames(th.Stacktrace.Frames, home)
		}
	}

	if event.Tags != nil {
		for _, key := range redactedTagKeys {
			delete(event.Tags, key)
		}
	}

	if event.Request != nil {
		event.Request.Headers = nil
		event.Request.Cookies = ""
	}

	return event
}

func scrubString(s, home string) string {
	if s == "" {
		return s
	}
	if home != "" {
		s = strings.ReplaceAll(s, home, "~")
	}
	return awsAccessKeyRe.ReplaceAllString(s, "[REDACTED-AWS-KEY]")
}

func scrubFrames(frames []sentry.Frame, home string) {
	for i := range frames {
		f := &frames[i]
		f.Filename = scrubString(f.Filename, home)
		f.AbsPath = scrubString(f.AbsPath, home)
	}
}

var homeDirOverride = ""

// SetHomeDirOverride is exposed for tests.
func SetHomeDirOverride(dir string) func() {
	prev := homeDirOverride
	homeDirOverride = dir
	return func() { homeDirOverride = prev }
}

func homeDir() string {
	if homeDirOverride != "" {
		return homeDirOverride
	}
	dir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return dir
}
