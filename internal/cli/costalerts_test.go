package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

// readServer answers every GET with the given envelope and records the path.
func readServer(t *testing.T, status int, envelope interface{}) (*httptest.Server, *string) {
	t.Helper()
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.RequestURI()
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(envelope)
	}))
	t.Cleanup(srv.Close)
	return srv, &path
}

func alertBody(enabled bool) map[string]interface{} {
	return map[string]interface{}{
		"id":        "8f2b1c4e-0000-4a1b-9c3d-5e6f70819234",
		"name":      "Production budget",
		"source_id": "123456789012",
		"params":    map[string]interface{}{"kind": "budget", "budget": 40000},
		"recipients": []interface{}{
			map[string]interface{}{"name": "Ana", "email": "ana@example.com"},
		},
		"slack_channel": "#finops",
		"enabled":       enabled,
		"paused_by":     "ana@example.com",
		"sent_to_me":    true,
	}
}

func listEnvelope(items ...interface{}) map[string]interface{} {
	return map[string]interface{}{"success": true, "data": items}
}

func useServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
}

func TestCostAlertsList(t *testing.T) {
	srv, path := readServer(t, http.StatusOK, listEnvelope(alertBody(true)))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *path != "/api/v1/cost-alerts" {
		t.Errorf("path = %q", *path)
	}
	for _, want := range []string{"Production budget", "123456789012", "budget", "enabled"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestCostAlertsListEmpty(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope())
	useServer(t, srv)

	out, _, err := executeCommand(t, "alerts", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "No cost alerts configured") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsListJSON(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope(alertBody(true)))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "list", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"source_id\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsListForbidden(t *testing.T) {
	srv, _ := readServer(t, http.StatusForbidden, map[string]interface{}{"detail": "no"})
	useServer(t, srv)

	_, _, err := executeCommand(t, "costalerts", "list")
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("err = %v, want the permission hint", err)
	}
}

// A body that is not JSON has to name the problem rather than print an empty table.
func TestCostAlertsListInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)
	useServer(t, srv)

	_, _, err := executeCommand(t, "costalerts", "list")
	if err == nil || !strings.Contains(err.Error(), "invalid JSON response") {
		t.Fatalf("err = %v", err)
	}
}

func TestCostAlertsListUnreachable(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope())
	srv.Close()
	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"

	_, _, err := executeCommand(t, "costalerts", "list")
	if err == nil {
		t.Fatal("expected a transport error")
	}
}

func TestCostAlertsGet(t *testing.T) {
	srv, path := readServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "get", "alert-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *path != "/api/v1/cost-alerts/alert-1" {
		t.Errorf("path = %q", *path)
	}
	for _, want := range []string{"Production budget", "ana@example.com", "#finops", "\"kind\":\"budget\""} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

// A paused alert names who paused it, and an alert with no destination says so.
func TestCostAlertsGetPausedWithoutRecipients(t *testing.T) {
	alert := alertBody(false)
	alert["recipients"] = []interface{}{"not an object"}
	alert["slack_channel"] = ""
	srv, _ := readServer(t, http.StatusOK, map[string]interface{}{"data": alert})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "get", "alert-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"paused", "ana@example.com", "none"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
	if strings.Contains(out.String(), "Slack channel") {
		t.Errorf("an alert with no channel should not print one: %q", out.String())
	}
}

func TestCostAlertsGetPausedByNobody(t *testing.T) {
	alert := alertBody(false)
	alert["paused_by"] = ""
	srv, _ := readServer(t, http.StatusOK, map[string]interface{}{"data": alert})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "get", "alert-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out.String(), "paused by") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsGetJSON(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, map[string]interface{}{"data": alertBody(true)})
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "get", "alert-1", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"params\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsSources(t *testing.T) {
	srv, path := readServer(t, http.StatusOK, listEnvelope(
		map[string]interface{}{"id": "123456789012", "label": "prod", "group": "accounts", "provider": "aws"},
		map[string]interface{}{"id": "view-1", "label": "EU only", "group": "views", "provider": nil},
	))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "sources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *path != "/api/v1/cost-alerts/sources" {
		t.Errorf("path = %q", *path)
	}
	for _, want := range []string{"123456789012", "prod", "views", "aws"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestCostAlertsSourcesEmpty(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope())
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "sources")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Connect an account first") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsSourcesJSON(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope(
		map[string]interface{}{"id": "view-1", "label": "EU only", "group": "views"},
	))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "sources", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"group\"") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsEvents(t *testing.T) {
	srv, path := readServer(t, http.StatusOK, listEnvelope(
		map[string]interface{}{
			"instance_key":  "AmazonRDS",
			"state":         "firing",
			"period":        "2026-09",
			"as_of":         "2026-09-10",
			"observed_usd":  41200.5,
			"threshold_usd": 40000.0,
			"cost_basis":    "net_amortized",
			"notified":      true,
			"created_at":    "2026-09-11T06:02:00Z",
		},
		map[string]interface{}{
			"instance_key":  "",
			"state":         "resolved",
			"observed_usd":  "39100.00",
			"threshold_usd": nil,
			"notified":      false,
			"created_at":    "not a date",
		},
	))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "events", "alert-1", "--limit", "10", "--offset", "20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(*path, "limit=10") || !strings.Contains(*path, "offset=20") {
		t.Errorf("path = %q", *path)
	}
	for _, want := range []string{"AmazonRDS", "41200.50", "net_amortized", "yes", "whole source", "39100.00", "no"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %q", want, out.String())
		}
	}
}

func TestCostAlertsEventsEmpty(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope())
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "events", "alert-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "has not fired yet") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsEventsJSON(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope(map[string]interface{}{"state": "firing"}))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "events", "alert-1", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "\"state\"") {
		t.Errorf("output = %q", out.String())
	}
}

// The envelope decides the shape, so a data array of the wrong kind must not panic.
func TestCostAlertsListSkipsEntriesThatAreNotObjects(t *testing.T) {
	srv, _ := readServer(t, http.StatusOK, listEnvelope("a string", alertBody(true)))
	useServer(t, srv)

	out, _, err := executeCommand(t, "costalerts", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "Production budget") {
		t.Errorf("output = %q", out.String())
	}
}

func TestCostAlertsListUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "costalerts", "list")
	if err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("err = %v, want the not authenticated hint", err)
	}
}

// The help renderer buckets by GroupID, so a command with none is filed away from
// the ones it belongs beside.
func TestCostAlertsIsACoreCommand(t *testing.T) {
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "costalerts" {
			if cmd.GroupID != groupCore {
				t.Errorf("GroupID = %q, want %q", cmd.GroupID, groupCore)
			}
			return
		}
	}
	t.Fatal("costalerts is not registered on the root command")
}

func TestCompactJSONOfAnUnencodableValue(t *testing.T) {
	if got := compactJSON(func() {}); got != "" {
		t.Errorf("compactJSON = %q, want empty", got)
	}
}
