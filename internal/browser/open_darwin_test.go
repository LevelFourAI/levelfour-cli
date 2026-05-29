package browser

import "testing"

func TestOpenURL(t *testing.T) {
	err := OpenURL("https://example.com")
	if err != nil {
		t.Fatalf("OpenURL returned error: %v", err)
	}
}
