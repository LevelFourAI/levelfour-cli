package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewSDKClient(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		c, err := NewSDKClient("https://api.levelfour.ai", "l4_test_validkey123456789", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.BaseURL != "https://api.levelfour.ai" {
			t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://api.levelfour.ai")
		}
		if c.SDK() == nil {
			t.Error("SDK() should not be nil")
		}
		if c.Raw() == nil {
			t.Error("Raw() should not be nil")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := NewSDKClient("http://remote.example.com", "l4_test_validkey123456789", "1.0.0")
		if err == nil {
			t.Error("expected error for insecure remote URL")
		}
	})

	t.Run("localhost allowed", func(t *testing.T) {
		c, err := NewSDKClient("http://localhost:8000", "l4_test_validkey123456789", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.BaseURL != "http://localhost:8000" {
			t.Errorf("BaseURL = %q", c.BaseURL)
		}
	})

	t.Run("invalid key format", func(t *testing.T) {
		_, err := NewSDKClient("https://api.levelfour.ai", "bad-key", "1.0.0")
		if err == nil {
			t.Error("expected error for invalid key format")
		}
	})
}

func TestNewUnauthSDKClient(t *testing.T) {
	c := NewUnauthSDKClient("https://api.levelfour.ai", "1.0.0")
	if c.SDK() != nil {
		t.Error("SDK() should be nil for unauth client")
	}
	if c.Raw() == nil {
		t.Error("Raw() should not be nil")
	}
	if c.BaseURL != "https://api.levelfour.ai" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestSDKCallsNameTheCLIInTheirUserAgent(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", "levelfour-go/0.4.0 go1.25")
	resp, err := sdkHTTPClient("1.0.0").Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()

	if got != "LevelFour-CLI/1.0.0" {
		t.Errorf("User-Agent = %q, want %q", got, "LevelFour-CLI/1.0.0")
	}
	if req.Header.Get("User-Agent") != "levelfour-go/0.4.0 go1.25" {
		t.Error("the caller's request must not be modified")
	}
}
