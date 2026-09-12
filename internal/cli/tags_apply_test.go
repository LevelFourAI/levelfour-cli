package cli

import (
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

const mutationJSON = `{"key":` + teamsDetailJSON + `,"job":{"id":"job_2","status":"queued","from_period":"2026-06","to_period":"2026-09","periods_done":0,"periods_total":4,"error":null,"created_at":"2026-09-10T00:00:00Z","finished_at":null}}`

func applyRoutes(extra map[string]tagsRoute) map[string]tagsRoute {
	routes := map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), teamsRoute: okRoute(teamsDetailJSON)}
	for k, v := range extra {
		routes[k] = v
	}
	return routes
}

func changedTeamsYAML() string {
	spec := strings.Replace(teamsYAML, "effective_from: \"2026-06\"\n", "", 1)
	spec = strings.Replace(spec, "operator: flexible_match", "operator: is", 1)
	return strings.Replace(spec, "configs:\n", "configs:\n  - value: shared\n    rules:\n      - providers: [gcp]\n", 1)
}

func TestTagsApplyNoChanges(t *testing.T) {
	tests := []struct {
		name string
		spec string
		args []string
		want string
	}{
		{"dry run", teamsYAML, []string{"--dry-run"}, "No changes: virtual tag Teams already matches"},
		{"without flags", teamsYAML, nil, "No changes"},
		{"effective_from left out keeps the current month", strings.Replace(teamsYAML, "effective_from: \"2026-06\"\n", "", 1), []string{"--dry-run"}, "No changes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveTags(t, applyRoutes(nil))
			args := append([]string{"tags", "apply", "-f", writeSpecFile(t, tt.spec)}, tt.args...)
			out, _, err := executeCommand(t, args...)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertContains(t, out.String(), tt.want)
			if srv.wrote() {
				t.Error("an unchanged key must not be written")
			}
			if lookup, _ := srv.last(http.MethodGet, "/api/v1/tags/keys"); lookup.query.Get("origin") != "virtual" {
				t.Errorf("lookup origin = %q, want virtual", lookup.query.Get("origin"))
			}
		})
	}
}

func TestTagsApplyNoChangesJSON(t *testing.T) {
	serveTags(t, applyRoutes(nil))
	out, errOut, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, teamsYAML), "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), `"action": "none"`, `"key_id": "vtk_teams01"`)
	assertContains(t, errOut.String(), "No changes")
}

func TestTagsApplyDryRunWithChanges(t *testing.T) {
	srv := serveTags(t, applyRoutes(nil))
	out, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()), "--dry-run")
	if !errors.Is(err, ErrIssuesFound) {
		t.Fatalf("error = %v, want ErrIssuesFound so the exit code is 2", err)
	}
	assertContains(t, out.String(),
		"Update virtual tag Teams (vtk_teams01)",
		"Configs:",
		"  + shared <- gcp",
		"    data <- aws, gcp where service is Amazon Redshift, BigQuery",
		"  - team-a <- aws, gcp where tag team flexibly matches Team A",
		"  + team-a <- aws, gcp where tag team is Team A",
		"       measured on aws where charge type is Usage",
		"Dry run: nothing was sent.",
	)
	assertNotContains(t, out.String(), "Settings:", "Collapsed keys:")
	if srv.wrote() {
		t.Error("a dry run must not write")
	}
}

func TestTagsApplyDryRunBadJQ(t *testing.T) {
	serveTags(t, applyRoutes(nil))
	_, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()), "--dry-run", "--jq", ".[")
	if err == nil || !strings.Contains(err.Error(), "invalid jq expression") {
		t.Fatalf("error = %v, want the jq error", err)
	}
}

func TestTagsApplyReplacesChangedRules(t *testing.T) {
	srv := serveTags(t, applyRoutes(map[string]tagsRoute{"PUT /api/v1/tags/virtual/vtk_teams01": okRoute(mutationJSON)}))
	out, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()), "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "Updated virtual tag Teams", "vtk_teams01", "queued, 2026-06..2026-09, 0 of 4 periods")
	req, ok := srv.last(http.MethodPut, "/api/v1/tags/virtual/vtk_teams01")
	if !ok {
		t.Fatal("expected a PUT")
	}
	if req.key == "" {
		t.Error("expected an Idempotency-Key header")
	}
	if req.body["expected_version"] != float64(4) || req.body["effective_from"] != "2026-06" || req.body["name"] != "Teams" {
		t.Errorf("body = %v, want expected_version 4 and the carried effective_from", req.body)
	}
	if configs, _ := req.body["configs"].([]interface{}); len(configs) != 5 {
		t.Errorf("configs = %d, want 5", len(configs))
	}
}

func TestTagsApplyPatchesSettingsOnly(t *testing.T) {
	spec := strings.Replace(teamsYAML, "Owning team across AWS and GCP.", "Owning team.", 1)
	spec = strings.Replace(spec, "can_override: false", "can_override: true", 1)
	srv := serveTags(t, applyRoutes(map[string]tagsRoute{"PATCH /api/v1/tags/virtual/vtk_teams01": okRoute(`{"key":` + teamsDetailJSON + `,"job":null}`)}))
	out, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, spec), "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(),
		`~ description: "Owning team across AWS and GCP." -> "Owning team."`,
		"~ can_override: false -> true",
		"Updated virtual tag Teams",
	)
	assertNotContains(t, out.String(), "Configs:", "Job")
	req, ok := srv.last(http.MethodPatch, "/api/v1/tags/virtual/vtk_teams01")
	if !ok {
		t.Fatal("expected a PATCH")
	}
	want := map[string]interface{}{"expected_version": float64(4), "description": "Owning team.", "can_override": true}
	if !reflect.DeepEqual(req.body, want) {
		t.Errorf("body = %v, want %v", req.body, want)
	}
	if req.key == "" {
		t.Error("expected an Idempotency-Key header")
	}
}

func TestTagsApplyCreates(t *testing.T) {
	const spec = `name: Environments
collapsed_keys:
  - source: {origin: provider, key: env}
  - source: {origin: provider, key: Environment}
    providers: [gcp]
    prefix: "gcp-"
`
	srv := serveTags(t, map[string]tagsRoute{
		keysRoute:                   okRoute(tagKeysJSON),
		"POST /api/v1/tags/virtual": {status: http.StatusCreated, body: `{"success":true,"data":{"key":{"id":"vtk_env09","name":"Environments"},"job":null}}`},
	})
	out, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, spec), "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(),
		"Create virtual tag Environments",
		"  + name: Environments",
		`  + description: ""`,
		"  + effective_from: current month",
		"  + can_override: false",
		"Collapsed keys:",
		"  + provider key env on every provider",
		`  + provider key Environment on gcp with prefix "gcp-"`,
		"Created virtual tag Environments",
		"vtk_env09",
	)
	req, ok := srv.last(http.MethodPost, "/api/v1/tags/virtual")
	if !ok || req.key == "" {
		t.Fatalf("expected a POST carrying an Idempotency-Key, got %+v", req)
	}
	if configs, isList := req.body["configs"].([]interface{}); !isList || len(configs) != 0 {
		t.Errorf("configs = %v, want an empty list", req.body["configs"])
	}
	if _, sent := req.body["expected_version"]; sent {
		t.Error("a create must not send expected_version")
	}
}

func TestTagsApplyJSONResult(t *testing.T) {
	serveTags(t, applyRoutes(map[string]tagsRoute{"PUT /api/v1/tags/virtual/vtk_teams01": okRoute(mutationJSON)}))
	out, errOut, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()), "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), `"job_2"`)
	assertContains(t, errOut.String(), "Update virtual tag Teams")
}

func TestTagsApplyDeclined(t *testing.T) {
	srv := serveTags(t, applyRoutes(nil))
	withTerminal(t, true)
	withStdin(t, "n\n")
	out, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "Apply these changes to virtual tag Teams? [y/N]", "Aborted.")
	if srv.wrote() {
		t.Error("a declined apply must not write")
	}
}

func TestTagsApplyErrors(t *testing.T) {
	boom := errRoute(http.StatusInternalServerError, `{"code":"internal","message":"boom"}`)
	tests := []struct {
		name    string
		spec    string
		noFile  bool
		routes  map[string]tagsRoute
		wantErr string
	}{
		{"no file", "", true, nil, "pass the definition file with -f FILE"},
		{"no name", "description: nameless\n", false, nil, "name is required"},
		{"lookup fails", teamsYAML, false, map[string]tagsRoute{keysRoute: boom}, "boom"},
		{"detail fails", teamsYAML, false, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), teamsRoute: boom}, "boom"},
		{
			"stale version", changedTeamsYAML(), false,
			applyRoutes(map[string]tagsRoute{"PUT /api/v1/tags/virtual/vtk_teams01": errRoute(http.StatusConflict, `{"code":"version_conflict","message":"Version conflict","details":{"current_version":5}}`)}),
			"It is now at version 5. " + versionConflictHint,
		},
		{
			"rejected definition", "name: Fresh\nconfigs:\n  - value: a\n    rules: [{where: []}]\n", false,
			map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), "POST /api/v1/tags/virtual": errRoute(http.StatusUnprocessableEntity, `{"code":"validation_failed","message":"The definition has 2 problems","details":{"problems":[{"code":"rule_no_provider","config_index":0,"rule_index":0},{"code":"key_empty","field":"name"}]}}`)},
			"rule_no_provider at config 1, rule 1\n  - key_empty at name",
		},
		{
			"unexpected result", "name: Fresh\n", false,
			map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), "POST /api/v1/tags/virtual": okRoute(`"nope"`)},
			"unexpected response",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serveTags(t, tt.routes)
			args := []string{"tags", "apply", "--yes"}
			if !tt.noFile {
				args = append(args, "-f", writeSpecFile(t, tt.spec))
			}
			_, _, err := executeCommand(t, args...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestDiffItems(t *testing.T) {
	item := func(lines ...string) []string { return lines }
	tests := []struct {
		name   string
		before [][]string
		after  [][]string
		want   []string
	}{
		{"identical", [][]string{item("a"), item("b")}, [][]string{item("a"), item("b")}, []string{" a", " b"}},
		{"insert at top", [][]string{item("a"), item("b")}, [][]string{item("z"), item("a"), item("b")}, []string{"+z", " a", " b"}},
		{"replace middle", [][]string{item("a"), item("b"), item("c")}, [][]string{item("a"), item("x"), item("c")}, []string{" a", "-b", "+x", " c"}},
		{"drop the tail", [][]string{item("a"), item("b")}, [][]string{item("a")}, []string{" a", "-b"}},
		{"everything new", nil, [][]string{item("a")}, []string{"+a"}},
		{"multi-line items compare whole", [][]string{item("a", "or b")}, [][]string{item("a", "or c")}, []string{"-a\nor b", "+a\nor c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := diffItems(tt.before, tt.after)
			got := make([]string, len(entries))
			for i, e := range entries {
				got[i] = e.mark + strings.Join(e.lines, "\n")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("diffItems() = %q, want %q", got, tt.want)
			}
		})
	}
}
