package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func virtualTagBreakdownServer(t *testing.T) (*httptest.Server, *url.Values) {
	t.Helper()
	var seen url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"}},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/breakdown"):
			seen = r.URL.Query()
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":       "aws",
					"provider_name":     "AWS",
					"period":            "2026-08",
					"start_date":        "2026-08-01",
					"end_date":          "2026-08-31",
					"total_period_cost": 120.0,
					"items": []interface{}{
						map[string]interface{}{"virtual_tag": "data", "cost": 70.0, "change_percentage": 5.0},
						map[string]interface{}{"virtual_tag": "__unallocated__", "cost": 30.0},
					},
				},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func runBreakdown(t *testing.T, srv *httptest.Server, args ...string) (string, error) {
	t.Helper()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()
	out, _, err := executeCommand(t, append([]string{"costs", "breakdown"}, args...)...)
	return out.String(), err
}

func TestCostsBreakdownGroupedByAVirtualTag(t *testing.T) {
	srv, seen := virtualTagBreakdownServer(t)

	got, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, got, "Virtual Tag", "data", "__unallocated__", "$70.00", "$30.00", "Period Total")
	if v := seen.Get("virtual_tag_key"); v != "Teams" {
		t.Errorf("virtual_tag_key = %q, want Teams", v)
	}
	if v := seen.Get("group_by"); v != "virtual_tag" {
		t.Errorf("group_by = %q, want virtual_tag", v)
	}
}

func TestCostsBreakdownFiltersByVirtualTagValue(t *testing.T) {
	srv, seen := virtualTagBreakdownServer(t)

	if _, err := runBreakdown(t, srv,
		"--group-by", "service", "--virtual-tag-key", "Teams", "--virtual-tag-value", "data",
		"--virtual-tag-value", "__unallocated__"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := (*seen)["virtual_tag_value"]; len(got) != 2 || got[0] != "data" || got[1] != "__unallocated__" {
		t.Errorf("virtual_tag_value = %v, want both values", got)
	}
}

func TestCostsBreakdownVirtualTagJSONPassesTheBodyThrough(t *testing.T) {
	srv, _ := virtualTagBreakdownServer(t)

	got, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, got, "virtual_tag", "__unallocated__")
}

func TestCostsBreakdownVirtualTagReportsAnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"}},
			})
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"detail":{"code":"virtual_tag_error","message":"Unknown virtual tag key: Teams"}}`))
	}))
	t.Cleanup(srv.Close)

	_, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams")
	if err == nil || !strings.Contains(err.Error(), "Unknown virtual tag key") {
		t.Fatalf("error = %v, want it to carry the API message", err)
	}
}

func TestCostsBreakdownVirtualTagReportsAnEmptyWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{"provider_id": "aws", "total_period_cost": 0.0, "items": []interface{}{}},
		})
	}))
	t.Cleanup(srv.Close)

	got, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, got, "No cost breakdown data found.")
}

func TestRenderVirtualTagBreakdownRejectsABodyItCannotRead(t *testing.T) {
	for _, body := range []string{`not json`, `{"data":null}`, `{"data":{"items":"not a list"}}`} {
		if err := renderVirtualTagBreakdown([]byte(body), nil); err == nil {
			t.Errorf("body %q: want an error", body)
		}
	}
}

func TestVirtualTagFlagsAreValidatedBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name    string
		state   costsFilterState
		wantErr string
	}{
		{
			"grouping by virtual_tag needs a key",
			costsFilterState{groupBy: []string{"virtual_tag"}},
			"--virtual-tag-key is required",
		},
		{
			"a value needs a key",
			costsFilterState{virtualTagValue: []string{"data"}},
			"--virtual-tag-value needs --virtual-tag-key",
		},
		{
			"a dimension the output rows do not carry",
			costsFilterState{groupBy: []string{"tag"}, tagKey: []string{"team"}, virtualTagKey: "Teams"},
			"cannot be combined with --virtual-tag-key",
		},
		{
			"a provider tag filter beside a virtual tag",
			costsFilterState{virtualTagKey: "Teams", tagValue: []string{"data"}},
			"--tag-key and --tag-value cannot be combined",
		},
		{
			"an unknown grouping still names every dimension",
			costsFilterState{groupBy: []string{"nonsense"}},
			"service, account_id, region, tag, virtual_tag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.validate()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestAVirtualTagKeyWithoutAGroupByIsAllowed(t *testing.T) {
	state := costsFilterState{virtualTagKey: "Teams"}
	if err := state.validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCostsBreakdownVirtualTagReportsATransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"}},
			})
			return
		}
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		conn.Close()
	}))
	t.Cleanup(srv.Close)

	if _, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams"); err == nil {
		t.Fatal("want an error when the request never completes")
	}
}

func TestVirtualTagBreakdownPrintsThePaginationFooter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"provider_id":       "aws",
				"total_period_cost": 70.0,
				"items":             []interface{}{map[string]interface{}{"virtual_tag": "data", "cost": 70.0}},
				"pagination": map[string]interface{}{
					"current_page": 1, "total_pages": 3, "total_items": 42, "has_next": true,
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	got, err := runBreakdown(t, srv, "--group-by", "virtual_tag", "--virtual-tag-key", "Teams")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, got, "42")
}

// The typed decode ignores the field, so only the second pass can object to its type.
func TestRenderVirtualTagBreakdownRejectsAVirtualTagThatIsNotAName(t *testing.T) {
	body := []byte(`{"data":{"provider_id":"aws","items":[{"cost":1,"virtual_tag":123}]}}`)
	if err := renderVirtualTagBreakdown(body, nil); err == nil {
		t.Fatal("want an error when virtual_tag is not a string")
	}
}
