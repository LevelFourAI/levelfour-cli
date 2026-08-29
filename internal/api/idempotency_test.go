package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewIdempotencyKey(t *testing.T) {
	tests := []struct {
		name  string
		check func(t *testing.T, keys []string)
	}{
		{
			name: "shaped as a v4 uuid",
			check: func(t *testing.T, keys []string) {
				for _, k := range keys {
					if !uuidV4Pattern.MatchString(k) {
						t.Errorf("key %q is not a version 4 UUID", k)
					}
				}
			},
		},
		{
			name: "unique per call",
			check: func(t *testing.T, keys []string) {
				seen := make(map[string]bool, len(keys))
				for _, k := range keys {
					if seen[k] {
						t.Errorf("duplicate key %q", k)
					}
					seen[k] = true
				}
			},
		},
	}

	keys := make([]string, 0, 64)
	for i := 0; i < 64; i++ {
		keys = append(keys, NewIdempotencyKey())
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, keys)
		})
	}
}

func TestDoRawWithHeaders(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		wantKey  string
		wantBody bool
	}{
		{"no extra headers", nil, "", false},
		{"idempotency key", map[string]string{"Idempotency-Key": "abc-123"}, "abc-123", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotKey, gotContentType string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotKey = r.Header.Get("Idempotency-Key")
				gotContentType = r.Header.Get("Content-Type")
				w.WriteHeader(200)
				w.Write([]byte(`{"success":true}`))
			}))
			defer srv.Close()

			c, _ := NewRawClient(srv.URL, "l4_test_testkey123456789a", "1.0.0")

			var body *strings.Reader
			if tt.wantBody {
				body = strings.NewReader(`{}`)
			}
			var resp *RawResponse
			var err error
			if body == nil {
				resp, err = c.DoRawWithHeaders("POST", "/write", nil, tt.headers)
			} else {
				resp, err = c.DoRawWithHeaders("POST", "/write", body, tt.headers)
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != 200 {
				t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
			}
			if gotKey != tt.wantKey {
				t.Errorf("Idempotency-Key = %q, want %q", gotKey, tt.wantKey)
			}
			if tt.wantBody && gotContentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", gotContentType)
			}
		})
	}
}
