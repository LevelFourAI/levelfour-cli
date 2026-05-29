//go:build windows

package tuicommon

// DrainTermInput is a no-op on Windows. The Unix /dev/tty open + nonblocking
// drain pattern has no direct Windows equivalent, and the TUI commands that
// call this are interactive ones rarely run on Windows. The function exists
// so callers can stay platform-agnostic.
func DrainTermInput() {}
