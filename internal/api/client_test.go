package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"https valid", "https://api.levelfour.ai", false},
		{"http localhost", "http://localhost:8000", false},
		{"http 127.0.0.1", "http://127.0.0.1:8000", false},
		{"http remote rejected", "http://api.example.com", true},
		{"invalid url", "://bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBaseURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBaseURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestToFloat(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
		want float64
	}{
		{"float64", float64(42.5), 42.5},
		{"int", int(10), 10.0},
		{"string returns zero", "not a number", 0},
		{"nil returns zero", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToFloat(tt.val)
			if got != tt.want {
				t.Errorf("ToFloat(%v) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestNewClient(t *testing.T) {
	t.Run("valid url", func(t *testing.T) {
		c, err := NewClient("https://api.levelfour.ai", "test-key", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.BaseURL != "https://api.levelfour.ai" {
			t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://api.levelfour.ai")
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		_, err := NewClient("http://remote.example.com", "key", "1.0.0")
		if err == nil {
			t.Error("expected error for insecure remote URL")
		}
	})
}

func TestNewUnauthenticatedClient(t *testing.T) {
	c := NewUnauthenticatedClient("https://api.levelfour.ai", "1.0.0")
	if c.BaseURL != "https://api.levelfour.ai" {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL, "https://api.levelfour.ai")
	}
	if c.apiKey != "" {
		t.Error("expected empty apiKey for unauthenticated client")
	}
}

func TestClientGet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("User-Agent") != "LevelFour-CLI/1.0.0" {
				t.Errorf("wrong user-agent: %s", r.Header.Get("User-Agent"))
			}
			if r.Header.Get("Accept") != "application/json" {
				t.Errorf("wrong accept header: %s", r.Header.Get("Accept"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok"})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "test-key", "1.0.0")
		resp, err := c.Get("/health")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp["status"] != "ok" {
			t.Errorf("expected status ok, got %v", resp["status"])
		}
	})

	t.Run("4xx structured error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(403)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{"message": "forbidden"},
			})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Get("/test")
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "API error (403): forbidden" {
			t.Errorf("error = %q, want %q", got, "API error (403): forbidden")
		}
	})

	t.Run("4xx plain error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
			w.Write([]byte("internal server error"))
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Get("/test")
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "API error (500): internal server error" {
			t.Errorf("error = %q, want %q", got, "API error (500): internal server error")
		}
	})

	t.Run("invalid json response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Get("/test")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("unauthenticated client skips auth header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("expected no auth header for unauthenticated client")
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}))
		defer srv.Close()

		c := NewUnauthenticatedClient(srv.URL, "1.0.0")
		_, err := c.Get("/test")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestClientGetNewRequestError(t *testing.T) {
	c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
	c.BaseURL = "://invalid"
	_, err := c.Get("/test")
	if err == nil {
		t.Error("expected error for invalid base URL in request")
	}
}

func TestClientDo4xxWithNonJSONStructuredError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": "not a map",
		})
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, "key", "1.0.0")
	_, err := c.Get("/test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientDoHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	srv.Close()

	c, _ := NewClient(srv.URL, "key", "1.0.0")
	_, err := c.Get("/test")
	if err == nil {
		t.Error("expected error when server is closed")
	}
}

type errorReader struct{}

func (r *errorReader) Read(_ []byte) (int, error) {
	return 0, fmt.Errorf("forced read error")
}

type errorBodyTransport struct{}

func (t *errorBodyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(&errorReader{}),
		Header:     make(http.Header),
	}, nil
}

func TestClientDoReadBodyError(t *testing.T) {
	c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
	c.httpClient = &http.Client{Transport: &errorBodyTransport{}}
	_, err := c.Get("/test")
	if err == nil {
		t.Error("expected error when response body read fails")
	}
}

func TestClientPostMarshalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer srv.Close()

	c, _ := NewClient(srv.URL, "key", "1.0.0")
	_, err := c.Post("/test", make(chan int))
	if err == nil {
		t.Error("expected error for unmarshalable payload")
	}
}

func TestDecode(t *testing.T) {
	type testDevice struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	type testPoll struct {
		Status string  `json:"status"`
		APIKey *string `json:"api_key"`
	}

	t.Run("success", func(t *testing.T) {
		raw := map[string]interface{}{
			"data": map[string]interface{}{
				"device_code":      "abc123",
				"user_code":        "TEST-CODE",
				"verification_uri": "https://example.com/device",
				"expires_in":       float64(900),
				"interval":         float64(5),
			},
		}
		var out testDevice
		if err := Decode(raw, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.DeviceCode != "abc123" {
			t.Errorf("DeviceCode = %q, want %q", out.DeviceCode, "abc123")
		}
		if out.UserCode != "TEST-CODE" {
			t.Errorf("UserCode = %q, want %q", out.UserCode, "TEST-CODE")
		}
		if out.Interval != 5 {
			t.Errorf("Interval = %d, want 5", out.Interval)
		}
	})

	t.Run("missing data field", func(t *testing.T) {
		raw := map[string]interface{}{"success": true}
		var out testDevice
		if err := Decode(raw, &out); err == nil {
			t.Error("expected error for missing data field")
		}
	})

	t.Run("poll response", func(t *testing.T) {
		raw := map[string]interface{}{
			"data": map[string]interface{}{
				"status":  "complete",
				"api_key": "l4_test_abc",
			},
		}
		var out testPoll
		if err := Decode(raw, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Status != "complete" {
			t.Errorf("Status = %q, want %q", out.Status, "complete")
		}
		if out.APIKey == nil || *out.APIKey != "l4_test_abc" {
			t.Errorf("APIKey = %v, want %q", out.APIKey, "l4_test_abc")
		}
	})

	t.Run("null data", func(t *testing.T) {
		raw := map[string]interface{}{"data": nil}
		var out testDevice
		if err := Decode(raw, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.DeviceCode != "" {
			t.Errorf("expected zero value, got %q", out.DeviceCode)
		}
	})

	t.Run("unmarshal error", func(t *testing.T) {
		raw := map[string]interface{}{
			"data": map[string]interface{}{
				"expires_in": "not-a-number",
			},
		}
		var out testDevice
		if err := Decode(raw, &out); err == nil {
			t.Error("expected error for type mismatch")
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		raw := map[string]interface{}{
			"data": make(chan int),
		}
		var out testDevice
		if err := Decode(raw, &out); err == nil {
			t.Error("expected error for unmarshalable data")
		}
	})
}

func TestClientDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "DELETE" {
				t.Errorf("expected DELETE, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"deleted": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "test-key", "1.0.0")
		resp, err := c.Delete("/resources/123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp["deleted"] != true {
			t.Errorf("expected deleted=true, got %v", resp["deleted"])
		}
	})

	t.Run("4xx error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error": map[string]interface{}{"message": "not found"},
			})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Delete("/resources/999")
		if err == nil {
			t.Fatal("expected error")
		}
		if got := err.Error(); got != "API error (404): not found" {
			t.Errorf("error = %q, want %q", got, "API error (404): not found")
		}
	})

	t.Run("new request error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.BaseURL = "://invalid"
		_, err := c.Delete("/test")
		if err == nil {
			t.Error("expected error for invalid base URL in DELETE request")
		}
	})
}

func TestClientPatch(t *testing.T) {
	t.Run("with payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PATCH" {
				t.Errorf("expected PATCH, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected json content-type, got %s", r.Header.Get("Content-Type"))
			}
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "updated" {
				t.Errorf("expected name=updated in body, got %v", body)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"patched": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		resp, err := c.Patch("/resources/1", map[string]string{"name": "updated"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp["patched"] != true {
			t.Errorf("expected patched=true, got %v", resp["patched"])
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "" {
				t.Error("expected no content-type for nil payload")
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Patch("/resources/1", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		_, err := c.Patch("/test", make(chan int))
		if err == nil {
			t.Error("expected error for unmarshalable payload")
		}
	})

	t.Run("new request error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.BaseURL = "://invalid"
		_, err := c.Patch("/test", map[string]string{"k": "v"})
		if err == nil {
			t.Error("expected error for invalid base URL in PATCH request")
		}
	})
}

func TestClientPut(t *testing.T) {
	t.Run("with payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "PUT" {
				t.Errorf("expected PUT, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected json content-type, got %s", r.Header.Get("Content-Type"))
			}
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "replaced" {
				t.Errorf("expected name=replaced in body, got %v", body)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"replaced": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		resp, err := c.Put("/resources/1", map[string]string{"name": "replaced"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp["replaced"] != true {
			t.Errorf("expected replaced=true, got %v", resp["replaced"])
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "" {
				t.Error("expected no content-type for nil payload")
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Put("/resources/1", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("marshal error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		_, err := c.Put("/test", make(chan int))
		if err == nil {
			t.Error("expected error for unmarshalable payload")
		}
	})

	t.Run("new request error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.BaseURL = "://invalid"
		_, err := c.Put("/test", map[string]string{"k": "v"})
		if err == nil {
			t.Error("expected error for invalid base URL in PUT request")
		}
	})
}

func TestClientDoRaw(t *testing.T) {
	t.Run("success with body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Errorf("missing or wrong auth header: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("User-Agent") != "LevelFour-CLI/1.0.0" {
				t.Errorf("wrong user-agent: %s", r.Header.Get("User-Agent"))
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected json content-type, got %s", r.Header.Get("Content-Type"))
			}
			w.Header().Set("X-Request-Id", "req-abc")
			w.WriteHeader(201)
			w.Write([]byte(`{"id":"new-resource"}`))
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "test-key", "1.0.0")
		body := bytes.NewReader([]byte(`{"name":"test"}`))
		raw, err := c.DoRaw("POST", "/resources", body)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if raw.StatusCode != 201 {
			t.Errorf("StatusCode = %d, want 201", raw.StatusCode)
		}
		if raw.Headers.Get("X-Request-Id") != "req-abc" {
			t.Errorf("X-Request-Id = %q, want %q", raw.Headers.Get("X-Request-Id"), "req-abc")
		}
		if string(raw.Body) != `{"id":"new-resource"}` {
			t.Errorf("Body = %q, want %q", string(raw.Body), `{"id":"new-resource"}`)
		}
	})

	t.Run("success without body", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Errorf("expected GET, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "" {
				t.Error("expected no content-type for nil body")
			}
			w.WriteHeader(200)
			w.Write([]byte(`{"data":"ok"}`))
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "test-key", "1.0.0")
		raw, err := c.DoRaw("GET", "/health", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if raw.StatusCode != 200 {
			t.Errorf("StatusCode = %d, want 200", raw.StatusCode)
		}
	})

	t.Run("unauthenticated client skips auth header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Error("expected no auth header for unauthenticated client")
			}
			w.WriteHeader(200)
			w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		c := NewUnauthenticatedClient(srv.URL, "1.0.0")
		_, err := c.DoRaw("GET", "/test", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("new request error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.BaseURL = "://invalid"
		_, err := c.DoRaw("GET", "/test", nil)
		if err == nil {
			t.Error("expected error for invalid base URL")
		}
	})

	t.Run("http error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
		srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.DoRaw("GET", "/test", nil)
		if err == nil {
			t.Error("expected error when server is closed")
		}
	})

	t.Run("read body error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.httpClient = &http.Client{Transport: &errorBodyTransport{}}
		_, err := c.DoRaw("GET", "/test", nil)
		if err == nil {
			t.Error("expected error when response body read fails")
		}
	})
}

func TestBuildQueryString(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		got := BuildQueryString(map[string]string{})
		if got != "" {
			t.Errorf("BuildQueryString(empty) = %q, want %q", got, "")
		}
	})

	t.Run("nil map", func(t *testing.T) {
		got := BuildQueryString(nil)
		if got != "" {
			t.Errorf("BuildQueryString(nil) = %q, want %q", got, "")
		}
	})

	t.Run("single param", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"status": "active"})
		if got != "?status=active" {
			t.Errorf("BuildQueryString = %q, want %q", got, "?status=active")
		}
	})

	t.Run("empty value skipped", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"status": "", "type": "ec2"})
		if got != "?type=ec2" {
			t.Errorf("BuildQueryString = %q, want %q", got, "?type=ec2")
		}
	})

	t.Run("all empty values", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"a": "", "b": ""})
		if got != "" {
			t.Errorf("BuildQueryString(all empty) = %q, want %q", got, "")
		}
	})

	t.Run("special characters encoded", func(t *testing.T) {
		got := BuildQueryString(map[string]string{"q": "hello world"})
		if got != "?q=hello+world" {
			t.Errorf("BuildQueryString = %q, want %q", got, "?q=hello+world")
		}
	})
}

func TestBuildQueryStringMulti(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		got := BuildQueryStringMulti(map[string][]string{})
		if got != "" {
			t.Errorf("BuildQueryStringMulti(empty) = %q, want %q", got, "")
		}
	})

	t.Run("nil map", func(t *testing.T) {
		got := BuildQueryStringMulti(nil)
		if got != "" {
			t.Errorf("BuildQueryStringMulti(nil) = %q, want %q", got, "")
		}
	})

	t.Run("single key multiple values", func(t *testing.T) {
		got := BuildQueryStringMulti(map[string][]string{"tag": {"a", "b"}})
		if got != "?tag=a&tag=b" {
			t.Errorf("BuildQueryStringMulti = %q, want %q", got, "?tag=a&tag=b")
		}
	})

	t.Run("empty values skipped", func(t *testing.T) {
		got := BuildQueryStringMulti(map[string][]string{"tag": {"a", "", "b"}})
		if got != "?tag=a&tag=b" {
			t.Errorf("BuildQueryStringMulti = %q, want %q", got, "?tag=a&tag=b")
		}
	})

	t.Run("all empty values", func(t *testing.T) {
		got := BuildQueryStringMulti(map[string][]string{"x": {"", ""}})
		if got != "" {
			t.Errorf("BuildQueryStringMulti(all empty) = %q, want %q", got, "")
		}
	})

	t.Run("special characters encoded", func(t *testing.T) {
		got := BuildQueryStringMulti(map[string][]string{"q": {"hello world"}})
		if got != "?q=hello+world" {
			t.Errorf("BuildQueryStringMulti = %q, want %q", got, "?q=hello+world")
		}
	})
}

func TestClientPost(t *testing.T) {
	t.Run("with payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("expected json content-type, got %s", r.Header.Get("Content-Type"))
			}
			var body map[string]interface{}
			json.NewDecoder(r.Body).Decode(&body)
			if body["key"] != "value" {
				t.Errorf("expected key=value in body, got %v", body)
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"created": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		resp, err := c.Post("/test", map[string]string{"key": "value"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp["created"] != true {
			t.Errorf("expected created=true, got %v", resp["created"])
		}
	})

	t.Run("new request error", func(t *testing.T) {
		c, _ := NewClient("https://api.levelfour.ai", "key", "1.0.0")
		c.BaseURL = "://invalid"
		_, err := c.Post("/test", map[string]string{"k": "v"})
		if err == nil {
			t.Error("expected error for invalid base URL in POST request")
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Content-Type") != "" {
				t.Error("expected no content-type for nil payload")
			}
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
		}))
		defer srv.Close()

		c, _ := NewClient(srv.URL, "key", "1.0.0")
		_, err := c.Post("/test", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
