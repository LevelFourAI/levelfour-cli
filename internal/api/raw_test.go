package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewRawClient(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		c, err := NewRawClient("https://api.levelfour.ai", "key", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.BaseURL != "https://api.levelfour.ai" {
			t.Errorf("BaseURL = %q", c.BaseURL)
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := NewRawClient("http://remote.example.com", "key", "1.0.0")
		if err == nil {
			t.Error("expected error for insecure remote URL")
		}
	})
}

func TestNewUnauthRawClient(t *testing.T) {
	c := NewUnauthRawClient("https://api.levelfour.ai", "1.0.0")
	if c.BaseURL != "https://api.levelfour.ai" {
		t.Errorf("BaseURL = %q", c.BaseURL)
	}
}

func TestRawClientDoRaw(t *testing.T) {
	t.Run("GET success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer l4_test_testkey123456789a" {
				t.Error("missing auth header")
			}
			if r.Header.Get("User-Agent") != "LevelFour-CLI/1.0.0" {
				t.Errorf("User-Agent = %q", r.Header.Get("User-Agent"))
			}
			w.WriteHeader(200)
			json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "l4_test_testkey123456789a", "1.0.0")
		resp, err := c.DoRaw("GET", "/health", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("POST with body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "application/json" {
				t.Error("missing content-type for POST with body")
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"ok":true}`))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "key", "1.0.0")
		resp, err := c.DoRaw("POST", "/test", strings.NewReader(`{"key":"val"}`))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.StatusCode != 200 {
			t.Errorf("StatusCode = %d", resp.StatusCode)
		}
	})

	t.Run("unauthenticated skips auth header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("should not have auth header for unauth client")
			}
			w.WriteHeader(200)
			w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		c := NewUnauthRawClient(srv.URL, "1.0.0")
		_, err := c.DoRaw("GET", "/health", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestRawClientDoRawConnectionError(t *testing.T) {
	c, _ := NewRawClient("https://localhost:1", "key", "1.0.0")
	_, err := c.DoRaw("GET", "/test", nil)
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestRawClientDoRawBadURL(t *testing.T) {
	c := NewUnauthRawClient("https://valid.example.com", "1.0.0")
	c.BaseURL = "://invalid"
	_, err := c.DoRaw("GET", "/test", nil)
	if err == nil {
		t.Error("expected error for bad URL")
	}
}

func TestRawResponseDecodeError(t *testing.T) {
	t.Run("structured error", func(t *testing.T) {
		r := &RawResponse{
			StatusCode: 401,
			Body:       []byte(`{"error":{"message":"unauthorized"}}`),
		}
		err := r.DecodeError()
		if !strings.Contains(err.Error(), "unauthorized") {
			t.Errorf("error = %q, want unauthorized", err.Error())
		}
	})

	t.Run("raw body fallback", func(t *testing.T) {
		r := &RawResponse{
			StatusCode: 500,
			Body:       []byte("internal server error"),
		}
		err := r.DecodeError()
		if !strings.Contains(err.Error(), "internal server error") {
			t.Errorf("error = %q", err.Error())
		}
	})
}

func TestAnalyzeIaCConnectionError(t *testing.T) {
	c, _ := NewRawClient("https://localhost:1", "key", "1.0.0")
	_, err := c.AnalyzeIaC(context.Background(), &AnalyzePrRequest{})
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestAnalyzeIaCBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c, _ := NewRawClient(srv.URL, "key", "1.0.0")
	_, err := c.AnalyzeIaC(context.Background(), &AnalyzePrRequest{})
	if err == nil {
		t.Error("expected decode error")
	}
}

func TestAnalyzeIaCNilData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"data": nil})
	}))
	defer srv.Close()

	c, _ := NewRawClient(srv.URL, "key", "1.0.0")
	_, err := c.AnalyzeIaC(context.Background(), &AnalyzePrRequest{})
	if err == nil {
		t.Error("expected error for nil data")
	}
}

func TestAnalyzeIaC(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v1/iac-analysis/analyze" || r.Method != "POST" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"cost_summary": map[string]interface{}{
						"total_monthly_difference": 42.0,
						"total_new_monthly":        42.0,
						"estimable_count":          1,
						"total_count":              1,
					},
				},
			})
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "key", "1.0.0")
		resp, err := c.AnalyzeIaC(context.Background(), &AnalyzePrRequest{
			Region: StringPtr("us-east-1"),
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.CostSummary == nil || resp.CostSummary.TotalMonthlyDifference != 42.0 {
			t.Error("expected cost summary with TotalMonthlyDifference=42")
		}
	})

	t.Run("api error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "key", "1.0.0")
		_, err := c.AnalyzeIaC(context.Background(), &AnalyzePrRequest{})
		if err == nil {
			t.Error("expected error for 401")
		}
	})
}

func TestCreateDeviceCodeBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c, _ := NewRawClient(srv.URL, "", "1.0.0")
	_, err := c.CreateDeviceCode(context.Background())
	if err == nil {
		t.Error("expected decode error")
	}
}

func TestCreateDeviceCodeConnectionError(t *testing.T) {
	c, _ := NewRawClient("https://localhost:1", "", "1.0.0")
	_, err := c.CreateDeviceCode(context.Background())
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestCreateDeviceCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"device_code":      "ABC123",
					"user_code":        "USER-CODE",
					"verification_uri": "https://example.com/verify",
					"expires_in":       300,
					"interval":         5,
				},
			})
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "", "1.0.0")
		resp, err := c.CreateDeviceCode(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Data.DeviceCode != "ABC123" {
			t.Errorf("DeviceCode = %q, want ABC123", resp.Data.DeviceCode)
		}
		if resp.Data.UserCode != "USER-CODE" {
			t.Errorf("UserCode = %q", resp.Data.UserCode)
		}
	})

	t.Run("invalid response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{"data": map[string]interface{}{}})
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "", "1.0.0")
		_, err := c.CreateDeviceCode(context.Background())
		if err == nil {
			t.Error("expected error for empty device code")
		}
	})

	t.Run("api error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte("error"))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "", "1.0.0")
		_, err := c.CreateDeviceCode(context.Background())
		if err == nil {
			t.Error("expected error for 500")
		}
	})
}

func TestPollDeviceCodeBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c, _ := NewRawClient(srv.URL, "", "1.0.0")
	_, err := c.PollDeviceCode(context.Background(), "ABC")
	if err == nil {
		t.Error("expected decode error")
	}
}

func TestPollDeviceCodeConnectionError(t *testing.T) {
	c, _ := NewRawClient("https://localhost:1", "", "1.0.0")
	_, err := c.PollDeviceCode(context.Background(), "ABC")
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestPollDeviceCode(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/v1/auth/device/") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"status":  "complete",
					"api_key": "l4_test_newkey1234567890",
				},
			})
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "", "1.0.0")
		resp, err := c.PollDeviceCode(context.Background(), "ABC123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Data.Status != "complete" {
			t.Errorf("Status = %q", resp.Data.Status)
		}
	})

	t.Run("api error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
			w.Write([]byte(`{"error":{"message":"not found"}}`))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "", "1.0.0")
		_, err := c.PollDeviceCode(context.Background(), "BAD")
		if err == nil {
			t.Error("expected error for 404")
		}
	})
}

func TestVerifyAuthConnectionError(t *testing.T) {
	c, _ := NewRawClient("https://localhost:1", "key", "1.0.0")
	err := c.VerifyAuth(context.Background())
	if err == nil {
		t.Error("expected connection error")
	}
}

func TestVerifyAuth(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
			w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "key", "1.0.0")
		err := c.VerifyAuth(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
			w.Write([]byte(`{"error":{"message":"unauthorized"}}`))
		}))
		defer srv.Close()

		c, _ := NewRawClient(srv.URL, "bad", "1.0.0")
		err := c.VerifyAuth(context.Background())
		if err == nil {
			t.Error("expected error for 401")
		}
	})
}

func TestStringPtr(t *testing.T) {
	t.Run("non-empty", func(t *testing.T) {
		p := StringPtr("hello")
		if p == nil || *p != "hello" {
			t.Errorf("StringPtr('hello') = %v", p)
		}
	})
	t.Run("empty", func(t *testing.T) {
		p := StringPtr("")
		if p != nil {
			t.Error("StringPtr('') should return nil")
		}
	})
}

func TestIntPtr(t *testing.T) {
	p := IntPtr(42)
	if p == nil || *p != 42 {
		t.Errorf("IntPtr(42) = %v", p)
	}
}
