package browser

import (
	"fmt"
	"testing"
)

func TestOpenURL_FirstCommand(t *testing.T) {
	origLookPath := lookPath
	origExecStart := execStart
	t.Cleanup(func() {
		lookPath = origLookPath
		execStart = origExecStart
	})

	lookPath = func(file string) (string, error) {
		if file == "xdg-open" {
			return "/usr/bin/xdg-open", nil
		}
		return "", fmt.Errorf("not found")
	}
	execStart = func(name, url string) error { return nil }

	if err := OpenURL("https://example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenURL_Fallback(t *testing.T) {
	origLookPath := lookPath
	origExecStart := execStart
	t.Cleanup(func() {
		lookPath = origLookPath
		execStart = origExecStart
	})

	lookPath = func(file string) (string, error) {
		if file == "wslview" {
			return "/usr/bin/wslview", nil
		}
		return "", fmt.Errorf("not found")
	}
	execStart = func(name, url string) error { return nil }

	if err := OpenURL("https://example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOpenURL_NoneFound(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(file string) (string, error) {
		return "", fmt.Errorf("not found")
	}

	err := OpenURL("https://example.com")
	if err == nil {
		t.Fatal("expected error when no browser found")
	}
}

func TestOpenURL_StartError(t *testing.T) {
	origLookPath := lookPath
	origExecStart := execStart
	t.Cleanup(func() {
		lookPath = origLookPath
		execStart = origExecStart
	})

	lookPath = func(file string) (string, error) {
		return "/usr/bin/" + file, nil
	}
	execStart = func(name, url string) error {
		return fmt.Errorf("start failed")
	}

	if err := OpenURL("https://example.com"); err == nil {
		t.Fatal("expected error when command fails to start")
	}
}

func TestExecStart_Default(t *testing.T) {
	if err := execStart("/bin/true", ""); err != nil {
		t.Fatalf("execStart with /bin/true failed: %v", err)
	}
}
