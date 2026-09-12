package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type tagsRoute struct {
	status int
	body   string
}

type tagsRequest struct {
	method string
	path   string
	query  url.Values
	key    string
	body   map[string]interface{}
}

type tagsServer struct {
	mu       sync.Mutex
	requests []tagsRequest
}

func serveTags(t *testing.T, routes map[string]tagsRoute) *tagsServer {
	t.Helper()
	s := &tagsServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := tagsRequest{method: r.Method, path: r.URL.EscapedPath(), query: r.URL.Query(), key: r.Header.Get("Idempotency-Key")}
		_ = json.Unmarshal(raw, &req.body)
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		route, ok := routes[r.Method+" "+r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"success":false,"error":{"code":"tag_not_found","message":"no such route"}}`))
			return
		}
		w.WriteHeader(route.status)
		w.Write([]byte(route.body))
	}))
	t.Cleanup(srv.Close)
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	return s
}

func (s *tagsServer) last(method, path string) (tagsRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.requests) - 1; i >= 0; i-- {
		if r := s.requests[i]; r.method == method && r.path == path {
			return r, true
		}
	}
	return tagsRequest{}, false
}

func (s *tagsServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *tagsServer) wrote() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.requests {
		if r.method != http.MethodGet {
			return true
		}
	}
	return false
}

func okRoute(data string) tagsRoute {
	return tagsRoute{status: http.StatusOK, body: `{"success":true,"data":` + data + `,"timestamp":"2026-09-10T00:00:00Z"}`}
}

func errRoute(status int, errorJSON string) tagsRoute {
	return tagsRoute{status: status, body: `{"success":false,"error":` + errorJSON + `}`}
}

func writeSpecFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tag.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing spec: %v", err)
	}
	return path
}

func assertContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func assertNotContains(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			t.Errorf("output should not contain %q:\n%s", u, got)
		}
	}
}

const (
	keysRoute     = "GET /api/v1/tags/keys"
	teamsRoute    = "GET /api/v1/tags/keys/vtk_teams01"
	providerRoute = "GET /api/v1/tags/keys/ptk_dGVhbQ"
)

const tagKeysJSON = `[
  {"id":"vtk_teams01","name":"Teams","origin":"virtual","description":"Owning team","providers":["aws","gcp"],"value_count":4,"resource_count":12,"spend":4750.5,"spend_share_pct":62.5,"status":"active","shares_provider_key":false,"values":[]},
  {"id":"ptk_dGVhbQ","name":"team","origin":"provider","description":"","providers":["aws"],"value_count":3,"resource_count":9,"spend":3100,"spend_share_pct":40.8,"status":null,"shares_provider_key":false,"values":[]},
  {"id":"vtk_apps02","name":"Applications","origin":"virtual","description":"","providers":[],"value_count":0,"resource_count":0,"spend":0,"spend_share_pct":0,"status":"reprocessing","shares_provider_key":false,"values":[]}
]`

const teamsDetailJSON = `{
  "id":"vtk_teams01","name":"Teams","origin":"virtual","description":"Owning team across AWS and GCP.",
  "providers":["aws","gcp"],"status":"reprocessing","shares_provider_key":false,"can_override":false,
  "effective_from":"2026-06","rules_version":4,
  "collapsed_keys":[{"source":{"origin":"provider","key":"team"},"scope":{"providers":["aws"]},"value_prefix":"","position":0}],
  "configs":[
    {"id":"cfg_1","position":0,"assign":{"type":"value","value":"data"},"filter":{"rules":[{"providers":["gcp","aws"],"conditions":[{"dimension":"service","key":null,"operator":"is","values":["Amazon Redshift","BigQuery"]}]}]},"time_frame":null},
    {"id":"cfg_2","position":1,"assign":{"type":"value","value":"team-a"},"filter":{"rules":[{"providers":["aws","gcp"],"conditions":[{"dimension":"tag","key":"team","operator":"flexible_match","values":["Team A"]}]}]},"time_frame":null},
    {"id":"cfg_3","position":2,"assign":{"type":"percent","splits":[{"value":"infra","pct":60},{"value":"mobile","pct":40}]},"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"account","key":null,"operator":"is","values":["999900001111"]}]}]},"time_frame":null},
    {"id":"cfg_4","position":3,"assign":{"type":"cost_based","input_filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"charge_type","key":null,"operator":"is","values":["Usage"]}]}]},"source":{"origin":"provider","key":"team"}},"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"service","key":null,"operator":"starts_with","values":["AWS Support"]}]}]},"time_frame":{"start":"2026-07-01","end":null}}
  ],
  "values":[{"id":"vtv_data","name":"data","spend":1200.5,"resource_count":4},{"id":"vtv_teama","name":"team-a","spend":800,"resource_count":3}],
  "total_spend":5000,"unallocated_spend":250.25,"resource_count":12,
  "dependents":[{"id":"vtk_cc03","name":"Cost Centers"}],
  "job":{"id":"job_1","status":"running","from_period":"2026-06","to_period":"2026-09","periods_done":2,"periods_total":4,"error":null,"created_at":"2026-09-10T00:00:00Z","finished_at":null},
  "window":{"start":"2026-08-11","end":"2026-09-09"}
}`

const nestedDetailJSON = `{
  "id":"vtk_nested1","name":"Nested","origin":"virtual","description":"","providers":["aws"],
  "status":"active","shares_provider_key":false,"can_override":false,"effective_from":"2026-06","rules_version":1,
  "collapsed_keys":[{"source":{"origin":"virtual","key":"vtk_apps02"},"scope":null,"value_prefix":"","position":0}],
  "configs":[
    {"id":"cfg_1","position":0,"assign":{"type":"value","value":"core"},"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"virtual_tag","key":"vtk_apps02","operator":"is","values":["checkout"]}]}]},"time_frame":null},
    {"id":"cfg_2","position":1,"assign":{"type":"cost_based","input_filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"virtual_tag","key":"vtk_apps02","operator":"is","values":["search"]}]}]},"source":{"origin":"virtual","key":"vtk_apps02"}},"filter":{"rules":[{"providers":["aws"],"conditions":[{"dimension":"service","key":null,"operator":"is","values":["AWS Support"]}]}]},"time_frame":null}
  ],
  "values":[],"total_spend":0,"unallocated_spend":0,"resource_count":0,"dependents":[],"job":null,
  "window":{"start":"2026-08-11","end":"2026-09-09"}
}`

const providerDetailJSON = `{"id":"ptk_dGVhbQ","name":"team","origin":"provider","description":"","providers":["aws"],"status":null,"rules_version":0,"collapsed_keys":[],"configs":[],"values":[],"total_spend":3100,"unallocated_spend":900,"resource_count":9,"dependents":[],"job":null,"window":{"start":"2026-08-11","end":"2026-09-09"}}`

// The file the docs publish; it describes exactly the key in teamsDetailJSON.
const teamsYAML = `name: Teams
description: Owning team across AWS and GCP.
effective_from: "2026-06"
can_override: false
collapsed_keys:
  - source: {origin: provider, key: team}
    providers: [aws]
    prefix: ""
configs:
  - value: data
    rules:
      - providers: [aws, gcp]
        where:
          - {dimension: service, operator: is, values: [Amazon Redshift, BigQuery]}
  - value: team-a
    rules:
      - providers: [aws, gcp]
        where:
          - {dimension: tag, key: team, operator: flexible_match, values: [Team A]}
  - split:
      - {value: infra, pct: 60}
      - {value: mobile, pct: 40}
    rules:
      - providers: [aws]
        where:
          - {dimension: account, operator: is, values: ["999900001111"]}
  - cost_based:
      source: {origin: provider, key: team}
      input:
        - providers: [aws]
          where:
            - {dimension: charge_type, operator: is, values: [Usage]}
    rules:
      - providers: [aws]
        where:
          - {dimension: service, operator: starts_with, values: [AWS Support]}
    time_frame: {start: "2026-07-01"}
`

func TestTagsInRootHelp(t *testing.T) {
	out, _, err := executeCommand(t, "--help")
	if err != nil {
		t.Fatalf("--help: %v", err)
	}
	assertContains(t, out.String(), "tags:")
}

func TestMatchTagKey(t *testing.T) {
	rows := []tagKeyRow{
		{ID: "ptk_dGVhbQ", Name: "team", Origin: tagOriginProvider},
		{ID: "vtk_team", Name: "Team", Origin: tagOriginVirtual},
		{ID: "ptk_ZW52", Name: "env", Origin: tagOriginProvider},
	}
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"TEAM", "vtk_team", true},
		{"Env", "ptk_ZW52", true},
		{"owner", "", false},
	}
	for _, tt := range tests {
		got, ok := matchTagKey(rows, tt.name)
		if ok != tt.wantOK || got.ID != tt.want {
			t.Errorf("matchTagKey(%q) = %q, %v; want %q, %v", tt.name, got.ID, ok, tt.want, tt.wantOK)
		}
	}
}

func TestDescribeJob(t *testing.T) {
	job := &tagJob{Status: "failed", FromPeriod: "2026-06", ToPeriod: "2026-09", PeriodsDone: 1, PeriodsTotal: 4}
	if got := describeJob(job); got != "failed, 2026-06..2026-09, 1 of 4 periods" {
		t.Errorf("describeJob() = %q", got)
	}
	job.Error = "source key missing"
	if got := describeJob(job); !strings.HasSuffix(got, ": source key missing") {
		t.Errorf("describeJob() = %q, want the error at the end", got)
	}
}

func TestTagsFlagValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"list origin", []string{"tags", "list", "--origin", "billing"}, `invalid --origin "billing"`},
		{"list provider", []string{"tags", "list", "--provider", "azure"}, `invalid --provider "azure"`},
		{"list start", []string{"tags", "list", "--start", "2026-13-01"}, `invalid --start "2026-13-01": use YYYY-MM-DD`},
		{"list end", []string{"tags", "list", "--end", "yesterday"}, `invalid --end "yesterday"`},
		{"list reversed window", []string{"tags", "list", "--start", "2026-08-31", "--end", "2026-08-01"}, "--end 2026-08-01 is before --start 2026-08-31"},
		{"show window", []string{"tags", "show", "Teams", "--start", "bad"}, `invalid --start "bad"`},
		{"coverage provider", []string{"tags", "coverage", "--provider", "azure"}, `invalid --provider "azure"`},
		{"coverage window", []string{"tags", "coverage", "--end", "bad"}, `invalid --end "bad"`},
		{"resources provider", []string{"tags", "resources", "Teams", "--provider", "k8s"}, `invalid --provider "k8s"`},
		{"costs provider", []string{"tags", "costs", "Teams", "--provider", "azure"}, "choose one of aws, gcp, all"},
		{"costs window", []string{"tags", "costs", "Teams", "--start", "bad"}, `invalid --start "bad"`},
		{"preview window", []string{"tags", "preview", "-f", "x.yaml", "--end", "bad"}, `invalid --end "bad"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveTags(t, nil)
			_, _, err := executeCommand(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if srv.count() != 0 {
				t.Error("an invalid flag must not reach the API")
			}
		})
	}
}

func TestTagsReadErrors(t *testing.T) {
	boom := errRoute(http.StatusInternalServerError, `{"code":"internal","message":"boom"}`)
	badData := okRoute(`"nope"`)
	tests := []struct {
		name    string
		args    []string
		routes  map[string]tagsRoute
		wantErr string
	}{
		{"list fails", []string{"tags", "list"}, map[string]tagsRoute{keysRoute: boom}, "boom"},
		{"list shape", []string{"tags", "list"}, map[string]tagsRoute{keysRoute: badData}, "unexpected response"},
		{"show lookup fails", []string{"tags", "show", "Teams"}, map[string]tagsRoute{keysRoute: boom}, "boom"},
		{"show unknown name", []string{"tags", "show", "Owner"}, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON)}, `no tag key named "Owner"`},
		{"show unknown id", []string{"tags", "show", "vtk_gone"}, nil, "no such route"},
		{"show shape", []string{"tags", "show", "vtk_teams01"}, map[string]tagsRoute{teamsRoute: badData}, "unexpected response"},
		{"coverage fails", []string{"tags", "coverage"}, map[string]tagsRoute{"GET /api/v1/tags/coverage": boom}, "boom"},
		{"coverage shape", []string{"tags", "coverage"}, map[string]tagsRoute{"GET /api/v1/tags/coverage": badData}, "unexpected response"},
		{"resources unknown name", []string{"tags", "resources", "Owner"}, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON)}, `no tag key named "Owner"`},
		{"resources fails", []string{"tags", "resources", "vtk_teams01"}, nil, "no such route"},
		{"resources shape", []string{"tags", "resources", "vtk_teams01"}, map[string]tagsRoute{"GET /api/v1/tags/keys/vtk_teams01/resources": badData}, "unexpected response"},
		{"resources value lookup fails", []string{"tags", "resources", "vtk_teams01", "--value", "data"}, nil, "no such route"},
		{"resources unknown value", []string{"tags", "resources", "vtk_teams01", "--value", "marketing"}, map[string]tagsRoute{teamsRoute: okRoute(teamsDetailJSON)}, `Teams has no value named "marketing"`},
		{"costs fails", []string{"tags", "costs", "Teams"}, map[string]tagsRoute{"GET /api/v1/costs/by-tag": boom}, "boom"},
		{"costs shape", []string{"tags", "costs", "Teams"}, map[string]tagsRoute{"GET /api/v1/costs/by-tag": badData}, "unexpected response"},
		{"preview without a file", []string{"tags", "preview"}, nil, "pass the definition file with -f FILE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serveTags(t, tt.routes)
			_, _, err := executeCommand(t, tt.args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestTagsReadJSON(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		routes map[string]tagsRoute
		want   string
	}{
		{"list", []string{"tags", "list", "--json"}, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON)}, `"vtk_apps02"`},
		{"show", []string{"tags", "show", "vtk_teams01", "--json"}, map[string]tagsRoute{teamsRoute: okRoute(teamsDetailJSON)}, `"rules_version": 4`},
		{"coverage", []string{"tags", "coverage", "--json"}, map[string]tagsRoute{"GET /api/v1/tags/coverage": okRoute(coverageJSON)}, `"with_virtual_pct": 93`},
		{"resources", []string{"tags", "resources", "vtk_teams01", "--json"}, map[string]tagsRoute{"GET /api/v1/tags/keys/vtk_teams01/resources": okRoute(resourcesJSON)}, `"vol-0def"`},
		{"costs", []string{"tags", "costs", "Teams", "--jq", ".data.unmapped.total"}, map[string]tagsRoute{"GET /api/v1/costs/by-tag": okRoute(byTagJSON)}, "250.25"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serveTags(t, tt.routes)
			out, _, err := executeCommand(t, tt.args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertContains(t, out.String(), tt.want)
		})
	}
}
