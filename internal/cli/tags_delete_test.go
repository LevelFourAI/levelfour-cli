package cli

import (
	"net/http"
	"strings"
	"testing"
)

const deleteTeamsRoute = "DELETE /api/v1/tags/virtual/vtk_teams01"

func TestTagsDeleteByName(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON), deleteTeamsRoute: {status: http.StatusNoContent}})
	out, _, err := executeCommand(t, "tags", "delete", "Teams", "--yes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), "Deleted virtual tag Teams")
	req, ok := srv.last(http.MethodDelete, "/api/v1/tags/virtual/vtk_teams01")
	if !ok {
		t.Fatal("expected a DELETE")
	}
	if req.key != "" {
		t.Error("a DELETE must not carry an Idempotency-Key")
	}
	if lookup, _ := srv.last(http.MethodGet, "/api/v1/tags/keys"); lookup.query.Get("origin") != "virtual" {
		t.Errorf("lookup origin = %q, want virtual", lookup.query.Get("origin"))
	}
}

func TestTagsDeleteJSON(t *testing.T) {
	serveTags(t, map[string]tagsRoute{deleteTeamsRoute: {status: http.StatusNoContent}})
	out, _, err := executeCommand(t, "tags", "delete", "vtk_teams01", "--yes", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertContains(t, out.String(), `"deleted": true`, `"id": "vtk_teams01"`)
}

func TestTagsDeleteConfirmation(t *testing.T) {
	tests := []struct {
		input      string
		wantDelete bool
		want       string
	}{
		{"n\n", false, "Aborted."},
		{"y\n", true, "Deleted virtual tag vtk_teams01"},
	}
	for _, tt := range tests {
		t.Run(strings.TrimSpace(tt.input), func(t *testing.T) {
			srv := serveTags(t, map[string]tagsRoute{deleteTeamsRoute: {status: http.StatusNoContent}})
			withTerminal(t, true)
			withStdin(t, tt.input)
			out, _, err := executeCommand(t, "tags", "delete", "vtk_teams01")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertContains(t, out.String(), "Delete virtual tag vtk_teams01? [y/N]", tt.want)
			if _, deleted := srv.last(http.MethodDelete, "/api/v1/tags/virtual/vtk_teams01"); deleted != tt.wantDelete {
				t.Errorf("deleted = %v, want %v", deleted, tt.wantDelete)
			}
		})
	}
}

func TestTagsDeleteErrors(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		routes  map[string]tagsRoute
		wantErr string
	}{
		{"provider key", "ptk_dGVhbQ", nil, "provider tags come from the bill and cannot be deleted"},
		{"unknown name", "Owner", map[string]tagsRoute{keysRoute: okRoute(tagKeysJSON)}, `no virtual tag key named "Owner"`},
		{
			"read by another key", "vtk_teams01",
			map[string]tagsRoute{deleteTeamsRoute: errRoute(http.StatusConflict, `{"code":"has_dependents","message":"Another key reads this key","details":{"dependents":[{"id":"vtk_cc03","name":"Cost Centers"},"Business Units"]}}`)},
			"Another key reads this key\nRead by: Cost Centers, Business Units",
		},
		{"read-only key", "vtk_teams01", map[string]tagsRoute{deleteTeamsRoute: errRoute(http.StatusForbidden, `{"code":"forbidden","message":"API key scope 'read' is insufficient"}`)}, "read-write key"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := serveTags(t, tt.routes)
			_, _, err := executeCommand(t, "tags", "delete", tt.ref, "--yes")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
			}
			if tt.routes == nil && srv.count() != 0 {
				t.Error("a provider key must be refused before any request")
			}
		})
	}
}

func TestTagsDeleteRefusesToRunUnattendedWithoutYes(t *testing.T) {
	srv := serveTags(t, map[string]tagsRoute{deleteTeamsRoute: {status: http.StatusNoContent}})
	withTerminal(t, false)

	_, _, err := executeCommand(t, "tags", "delete", "vtk_teams01")

	if err == nil || !strings.Contains(err.Error(), "outside a terminal needs --yes") {
		t.Fatalf("error = %v, want it to name --yes", err)
	}
	if srv.count() != 0 {
		t.Error("nothing must be sent, not even the key lookup")
	}
}

func TestTagsApplyRefusesToRunUnattendedWithoutYes(t *testing.T) {
	srv := serveTags(t, applyRoutes(nil))
	withTerminal(t, false)

	_, _, err := executeCommand(t, "tags", "apply", "-f", writeSpecFile(t, changedTeamsYAML()))

	if err == nil || !strings.Contains(err.Error(), "applying Teams outside a terminal needs --yes") {
		t.Fatalf("error = %v, want it to name --yes", err)
	}
	if srv.wrote() {
		t.Error("nothing must be written")
	}
}
