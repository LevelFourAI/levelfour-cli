//go:build !darwin && !linux && !windows

package browser

import "testing"

func TestOpenURL(t *testing.T) {
	if err := OpenURL("https://example.com"); err == nil {
		t.Fatal("expected error on unsupported platform")
	}
}
