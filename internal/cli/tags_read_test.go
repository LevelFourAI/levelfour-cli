package cli

import (
	"net/http"
	"strings"
	"testing"
)

const coverageJSON = `{"window":{"start":"2026-08-11","end":"2026-09-09"},
  "by_spend":{"total":182340.12,"provider_tagged":131200.00,"with_virtual":170100.50,"provider_tagged_pct":72,"with_virtual_pct":93},
  "by_resource":{"total":15,"provider_tagged":10,"with_virtual":12,"provider_tagged_pct":67,"with_virtual_pct":80}}`

const resourcesJSON = `{"items":[
  {"resource_id":"i-0abc","name":"web-1","type":"instance","provider":"aws","account_id":"123456789012","value_id":"vtv_data","value_name":"data","value_source":"config","ratio":1.0,"spend":120.5},
  {"resource_id":"vol-0def","name":"vol-0def","type":"volume","provider":"aws","account_id":"123456789012","value_id":"vtv_infra","value_name":"infra","value_source":"allocation","ratio":0.6,"spend":30},
  {"resource_id":"logs-bucket","name":"logs-bucket","type":"","provider":"gcp","account_id":"","value_id":null,"value_name":null,"value_source":null,"ratio":1.0,"spend":5}
],"pagination":{"total_items":11,"total_pages":3,"current_page":2,"page_size":5,"has_next":true,"has_previous":true}}`

const byTagJSON = `{"tag_key":"Teams","teams":[
  {"id":"data","label":"data","total":1200.5,"total_pct":24.0,"categories":{"compute":1200.5,"storage":0,"network":0,"database":0,"other":0}},
  {"id":"team-a","label":"team-a","total":800,"total_pct":16,"categories":{"compute":0,"storage":800,"network":0,"database":0,"other":0}}
],"unmapped":{"total":250.25}}`

const previewJSON = `{"window":{"start":"2026-08-01","end":"2026-08-31"},"total_spend":5000,"unallocated_spend":250,"shadowed_spend":75.5,
  "values":[{"name":"data","spend":1200,"resource_count":4,"sample_resources":["r1","r2","r3","r4","r5"]},{"name":"infra","spend":600,"resource_count":0,"sample_resources":[]}]}`

func TestTagsList(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON)})
	out, _, err := executeCommand(t, "tags", "list", "--origin", "virtual", "--provider", "aws", "--search", "team", "--start", "2026-08-01", "--end", "2026-08-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "Teams", "vtk_teams01", "$4750.50", "62.5%", "aws, gcp", "active", "Applications", "reprocessing")
	req, _ := srv.last(http.MethodGet, "/api/v1/tags/keys")
	for param, want := range map[string]string{"origin": "virtual", "provider": "aws", "search": "team", "start": "2026-08-01", "end": "2026-08-31"} {
		if got := req.query.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestTagsListEmpty(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{keysRoute: okRoute(`[]`)})
	out, _, err := executeCommand(t, "tags", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "No tag keys found.")
	if req, _ := srv.last(http.MethodGet, "/api/v1/tags/keys"); len(req.query) != 0 {
		t.Errorf("query = %v, want none without flags", req.query)
	}
}

func TestTagsShowVirtualKeyByName(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), teamsRoute: okRoute(teamsDetailJSON)})
	out, _, err := executeCommand(t, "tags", "show", "teams", "--start", "2026-08-01", "--end", "2026-08-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(),
		"vtk_teams01",
		"Owning team across AWS and GCP.",
		"2026-06",
		"running, 2026-06..2026-09, 2 of 4 periods",
		"Cost Centers",
		"$5000.00", "$250.25",
		"  1. provider key team on aws",
		"  1. data <- aws, gcp where service is Amazon Redshift, BigQuery",
		"  2. team-a <- aws, gcp where tag team flexibly matches Team A",
		"  3. infra 60%, mobile 40% <- aws where account is 999900001111",
		"  4. split by provider key team <- aws where service starts with AWS Support (from 2026-07-01)",
		"     measured on aws where charge type is Usage",
		"$1200.50",
	)
	req, ok := srv.last(http.MethodGet, "/api/v1/tags/keys/vtk_teams01")
	if !ok || req.query.Get("start") != "2026-08-01" || req.query.Get("end") != "2026-08-31" {
		t.Errorf("detail request = %+v, want the window passed on", req)
	}
	if lookup, _ := srv.last(http.MethodGet, "/api/v1/tags/keys"); len(lookup.query) != 0 {
		t.Errorf("name lookup query = %v, want every key", lookup.query)
	}
}

func TestTagsShowProviderKeyByID(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{providerRoute: okRoute(providerDetailJSON)})
	out, _, err := executeCommand(t, "tags", "show", "ptk_dGVhbQ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "team", "$900.00", "No value holds spend in this window.")
	assertNotContains(t, out.String(), "Effective from", "Configs, first match wins", "Read by")
	if _, looked := srv.last(http.MethodGet, "/api/v1/tags/keys"); looked {
		t.Error("an id must not trigger a name lookup")
	}
}

func TestTagsCoverage(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{"GET /api/v1/tags/coverage": okRoute(coverageJSON)})
	out, _, err := executeCommand(t, "tags", "coverage", "--provider", "aws")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "2026-08-11 to 2026-09-09", "$182340.12", "$131200.00 (72%)", "$170100.50 (93%)", "7%", "10 (67%)", "12 (80%)", "20%")
	if req, _ := srv.last(http.MethodGet, "/api/v1/tags/coverage"); req.query.Get("provider") != "aws" {
		t.Errorf("provider = %q, want aws", req.query.Get("provider"))
	}
}

func TestTagsResourcesWithValueName(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{
		keysRoute:  okRoute(tagKeysJSON),
		teamsRoute: okRoute(teamsDetailJSON),
		"GET /api/v1/tags/keys/vtk_teams01/resources": okRoute(resourcesJSON),
	})
	out, _, err := executeCommand(t, "tags", "resources", "Teams", "--value", "DATA", "--provider", "aws", "--search", "web", "--page", "2", "--page-size", "5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "i-0abc", "web-1", "infra (60%)", "logs-bucket", "$120.50", "Page 2 of 3 (11 total)")
	req, _ := srv.last(http.MethodGet, "/api/v1/tags/keys/vtk_teams01/resources")
	for param, want := range map[string]string{"value_id": "vtv_data", "provider": "aws", "search": "web", "page": "2", "page_size": "5"} {
		if got := req.query.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestTagsResourcesWithValueID(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{
		teamsRoute: okRoute(teamsDetailJSON),
		"GET /api/v1/tags/keys/vtk_teams01/resources": okRoute(`{"items":[],"pagination":{"total_items":0,"total_pages":0,"current_page":1,"page_size":20,"has_next":false}}`),
	})
	out, _, err := executeCommand(t, "tags", "resources", "vtk_teams01", "--value", "vtv_teama")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "No resources found.")
	req, _ := srv.last(http.MethodGet, "/api/v1/tags/keys/vtk_teams01/resources")
	if req.query.Get("value_id") != "vtv_teama" || req.query.Get("page") != "1" || req.query.Get("page_size") != "20" {
		t.Errorf("query = %v, want value_id vtv_teama and the default page", req.query)
	}
}

func TestTagsCosts(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{"GET /api/v1/costs/by-tag": okRoute(byTagJSON)})
	out, _, err := executeCommand(t, "tags", "costs", "Teams", "--provider", "all", "--start", "2026-08-01", "--end", "2026-08-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "Teams", "$2000.50", "$250.25", "data", "$1200.50", "24.0%", "16.0%")
	req, _ := srv.last(http.MethodGet, "/api/v1/costs/by-tag")
	for param, want := range map[string]string{"tag_key": "Teams", "provider": "all", "start": "2026-08-01", "end": "2026-08-31"} {
		if got := req.query.Get(param); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestTagsCostsNoValues(t *testing.T) {
	serveTags(t, map[string]tagsRoute{"GET /api/v1/costs/by-tag": okRoute(`{"tag_key":"team","teams":[],"unmapped":{"total":42}}`)})
	out, _, err := executeCommand(t, "tags", "costs", "team")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "$42.00", "No spend holds a value of this key in the window.")
}

func TestTagsPreview(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{"POST /api/v1/tags/virtual/preview": okRoute(previewJSON)})
	path := writeSpecFile(t, teamsYAML)
	out, _, err := executeCommand(t, "tags", "preview", "-f", path, "--start", "2026-08-01", "--end", "2026-08-31")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "2026-08-01 to 2026-08-31", "$75.50", "r1, r2, r3", "$600.00")
	assertNotContains(t, out.String(), "r4")
	req, _ := srv.last(http.MethodPost, "/api/v1/tags/virtual/preview")
	if req.key != "" {
		t.Error("a preview stores nothing and must not carry an Idempotency-Key")
	}
	if req.body["name"] != "Teams" || req.body["start"] != "2026-08-01" || req.body["end"] != "2026-08-31" {
		t.Errorf("body = %v", req.body)
	}
	if configs, _ := req.body["configs"].([]interface{}); len(configs) != 4 {
		t.Errorf("configs = %v, want 4", req.body["configs"])
	}
}

func TestTagsPreviewOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		route   tagsRoute
		json    bool
		want    string
		wantErr string
	}{
		{"nothing matched", okRoute(`{"window":{"start":"2026-08-01","end":"2026-08-31"},"total_spend":10,"unallocated_spend":10,"shadowed_spend":0,"values":[]}`), false, "No config matched any spend in the window.", ""},
		{"json", okRoute(previewJSON), true, `"shadowed_spend": 75.5`, ""},
		{"shape", okRoute(`"nope"`), false, "", "unexpected response"},
		{"rejected", errRoute(http.StatusUnprocessableEntity, `{"code":"validation_failed","message":"The definition has problems","details":{"problems":[{"code":"split_not_100","config_index":2,"split_index":0}]}}`), false, "", "split_not_100 at config 3, split 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serveTags(t, map[string]tagsRoute{"POST /api/v1/tags/virtual/preview": tt.route})
			args := []string{"tags", "preview", "-f", writeSpecFile(t, teamsYAML)}
			if tt.json {
				args = append(args, "--json")
			}
			out, _, err := executeCommand(t, args...)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertContains(t, out.String(), tt.want)
		})
	}
}

func TestTagsShowResolvesNestedKeyIdsToNames(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{
		keysRoute:                           okRoute(tagKeysJSON),
		"GET /api/v1/tags/keys/vtk_nested1": okRoute(nestedDetailJSON),
	})
	out, _, err := executeCommand(t, "tags", "show", "vtk_nested1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "virtual tag Applications", "virtual key Applications")
	assertNotContains(t, out.String(), "vtk_apps02")
	if _, looked := srv.last(http.MethodGet, "/api/v1/tags/keys"); !looked {
		t.Error("a detail naming another key must read the key list to render its name")
	}
}

func TestTagsShowSkipsTheKeyListWhenNothingNamesAKey(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{teamsRoute: okRoute(teamsDetailJSON)})
	if _, _, err := executeCommand(t, "tags", "show", "vtk_teams01"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, looked := srv.last(http.MethodGet, "/api/v1/tags/keys"); looked {
		t.Error("a detail with no nested reference must not read the key list")
	}
}

func TestTagsShowReportsAFailedKeyListLookup(t *testing.T) {
	serveTags(t, map[string]tagsRoute{
		keysRoute:                           errRoute(http.StatusInternalServerError, `{"code":"internal","message":"boom"}`),
		"GET /api/v1/tags/keys/vtk_nested1": okRoute(nestedDetailJSON),
	})
	_, _, err := executeCommand(t, "tags", "show", "vtk_nested1")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to contain boom", err)
	}
}
