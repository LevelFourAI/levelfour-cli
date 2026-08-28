package mcp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeFetcher records the request paths the executor builds and replays canned
// payloads, so a test can assert on the exact query string a tool produced.
type fakeFetcher struct {
	paths    []string
	payloads map[string]any
	fallback any
	err      error
}

func (f *fakeFetcher) Fetch(path string) (any, error) {
	f.paths = append(f.paths, path)
	if f.err != nil {
		return nil, f.err
	}
	base := path
	if i := strings.Index(path, "?"); i >= 0 {
		base = path[:i]
	}
	if payload, ok := f.payloads[base]; ok {
		return payload, nil
	}
	if f.fallback != nil {
		return f.fallback, nil
	}
	return map[string]any{}, nil
}

func toolNamed(t *testing.T, name string) tool {
	t.Helper()
	for _, tl := range tools {
		if tl.name == name {
			return tl
		}
	}
	t.Fatalf("no tool named %q", name)
	return tool{}
}

func TestRunSingleCallLiftsPayload(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/costs/daily/breakdown": map[string]any{"daily_spending": []any{map[string]any{"day": "2026-08-01"}}},
	}}
	got, err := toolNamed(t, "get-cost-series").run(f, map[string]any{"start": "2026-08-01"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(got["daily_spending"].([]any)) != 1 {
		t.Errorf("result = %v", got)
	}
	if f.paths[0] != "/api/v1/costs/daily/breakdown?start=2026-08-01" {
		t.Errorf("path = %q", f.paths[0])
	}
}

func TestRunNestsMultipleCallsUnderTheirKeys(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/costs/summary":          map[string]any{"total": float64(10)},
		"/api/v1/costs/monthly-spending": map[string]any{"months": []any{}},
	}}
	got, err := toolNamed(t, "get-cost-summary").run(f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["summary"].(map[string]any)["total"] != float64(10) {
		t.Errorf("summary = %v", got["summary"])
	}
	if got["monthly"] == nil {
		t.Error("monthly series missing")
	}
}

func TestRunPropagatesFetchErrors(t *testing.T) {
	f := &fakeFetcher{err: errors.New("API error (401): Unauthorized")}
	if _, err := toolNamed(t, "get-cost-summary").run(f, nil); err == nil {
		t.Fatal("expected the REST error to reach the caller")
	}
}

func TestCostsByTagListsKeysWhenNoTagKeyGiven(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/costs/by-tag/keys": map[string]any{"tag_keys": []any{"team", "owner"}},
	}}
	got, err := toolNamed(t, "get-costs-by-tag").run(f, map[string]any{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	keys := got["available_tag_keys"].([]any)
	if len(keys) != 2 {
		t.Errorf("available_tag_keys = %v", got["available_tag_keys"])
	}
	if len(f.paths) != 1 || !strings.HasPrefix(f.paths[0], "/api/v1/costs/by-tag/keys") {
		t.Errorf("paths = %v", f.paths)
	}
}

func TestCostsByTagEchoesTheKeyItGrouped(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/costs/by-tag":     map[string]any{"items": []any{map[string]any{"tag_value": "platform"}}},
		"/api/v1/costs/allocation": map[string]any{"coverage": float64(72)},
	}}
	got, err := toolNamed(t, "get-costs-by-tag").run(f, map[string]any{"tag_key": "team"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["tag_key"] != "team" {
		t.Errorf("tag_key = %v", got["tag_key"])
	}
	byTag := got["by_tag"].(map[string]any)
	if byTag["returned"] != 1 || byTag["total"] != 1 {
		t.Errorf("by_tag was not bounded: %v", byTag)
	}
	if got["allocation"] == nil {
		t.Error("allocation coverage missing, which is the figure that stops an untagged total being reported as a team total")
	}
}

// TestAccountsListPagesTheRESTListing covers the tool that turns an account id in
// a cost row into a name. GET /api/v1/accounts answers with its own pagination
// object, which has to be replaced by the shared envelope rather than sitting
// next to it: two answers to "is there another page" is worse than none.
func TestAccountsListPagesTheRESTListing(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/accounts": map[string]any{
			"items": []any{map[string]any{"account_id": "123456789012", "name": "data"}},
			"pagination": map[string]any{
				"total_items": float64(1), "current_page": float64(1), "has_next": true,
			},
		},
	}}
	got, err := toolNamed(t, "list-accounts").run(f, map[string]any{"page_size": float64(25)})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.paths[0] != "/api/v1/accounts?page=1&page_size=25" {
		t.Errorf("path = %q", f.paths[0])
	}
	if _, present := got["pagination"]; present {
		t.Errorf("the REST pagination object survived alongside the envelope: %v", got)
	}
	if got["page_size"] != 25 || got["returned"] != 1 || got["has_next_page"] != false {
		t.Errorf("pagination envelope = %v", got)
	}
	if got["hint"] != nil {
		t.Errorf("a page with rows on it carried an empty hint: %v", got["hint"])
	}
}

func TestAccountsListHintsWhenNothingIsConnected(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/accounts": map[string]any{"items": []any{}},
	}}
	got, err := toolNamed(t, "list-accounts").run(f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.paths[0] != "/api/v1/accounts?page=1&page_size=10" {
		t.Errorf("path = %q", f.paths[0])
	}
	hint, _ := got["hint"].(string)
	if !strings.Contains(hint, "No cloud accounts are connected") {
		t.Errorf("hint = %v, so a model cannot tell an empty tenant from a broken tool", got["hint"])
	}
}

func TestRecommendationsListTemplatesTheProviderAndRepeatsFilters(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/providers/gcp/recommendations": map[string]any{"items": []any{}, "total": float64(0)},
	}}
	got, err := toolNamed(t, "list-recommendations").run(f, map[string]any{
		"provider":    "gcp",
		"service":     []any{"RDS", "EC2"},
		"environment": "production",
		"page_size":   float64(5),
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	path := f.paths[0]
	if !strings.HasPrefix(path, "/api/v1/providers/gcp/recommendations?") {
		t.Errorf("provider was not templated into the path: %q", path)
	}
	for _, want := range []string{"service=RDS&service=EC2", "environment=production", "page_size=5"} {
		if !strings.Contains(path, want) {
			t.Errorf("path %q missing %q", path, want)
		}
	}
	if got["hint"] == nil {
		t.Error("an empty page carried no hint, so a model cannot tell it from a broken tool")
	}
	if got["page_size"] != 5 || got["has_next_page"] != false {
		t.Errorf("pagination envelope = %v", got)
	}
}

func TestRecommendationsGetEscapesTheIDAndSlimsTheDetail(t *testing.T) {
	f := &fakeFetcher{fallback: map[string]any{
		"recommendation_id": "REC-1234",
		"iam_policy":        "secret",
	}}
	got, err := toolNamed(t, "get-recommendation").run(f,
		map[string]any{"recommendation_id": "CLICK 243/../etc"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(f.paths[0], "/../") || strings.Contains(f.paths[0], " ") {
		t.Errorf("path was not escaped: %q", f.paths[0])
	}
	if _, present := got["iam_policy"]; present {
		t.Error("the IAM policy reached the model")
	}
}

func TestDefaultsAreSentAndAbsentOptionalsAreOmitted(t *testing.T) {
	f := &fakeFetcher{}
	if _, err := toolNamed(t, "get-cost-forecast").run(f, nil); err != nil {
		t.Fatalf("run: %v", err)
	}
	if f.paths[0] != "/api/v1/costs/forecast?horizon=eom&provider=aws" {
		t.Errorf("path = %q", f.paths[0])
	}

	f = &fakeFetcher{}
	if _, err := toolNamed(t, "list-commitments").run(f, map[string]any{"provider": nil}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(f.paths[0], "provider=") {
		t.Errorf("an omitted optional was sent anyway: %q", f.paths[0])
	}
}

func TestAnomaliesListShapesBothCalls(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{
		"/api/v1/anomalies":         map[string]any{"items": []any{}, "total_count": float64(0)},
		"/api/v1/anomalies/summary": map[string]any{"critical": float64(0)},
	}}
	got, err := toolNamed(t, "list-anomalies").run(f, map[string]any{"severity": "critical"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	anomalies := got["anomalies"].(map[string]any)
	if anomalies["hint"] == nil {
		t.Error("empty anomalies carried no hint")
	}
	if got["summary"] == nil {
		t.Error("summary missing")
	}
	if !strings.Contains(f.paths[0], "severity=critical") {
		t.Errorf("severity filter not sent: %q", f.paths[0])
	}
}

func TestNonObjectPayloadsAreNamedRatherThanDropped(t *testing.T) {
	f := &fakeFetcher{fallback: []any{"a list, not an object"}}
	got, err := toolNamed(t, "get-cost-series").run(f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["data"] == nil {
		t.Errorf("list payload was dropped: %v", got)
	}
}

func TestPickIgnoresANonObjectPayload(t *testing.T) {
	f := &fakeFetcher{payloads: map[string]any{"/api/v1/costs/by-tag/keys": []any{"team"}}}
	got, err := toolNamed(t, "get-costs-by-tag").run(f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["available_tag_keys"] == nil {
		t.Errorf("result = %v", got)
	}
}

func TestRowsKeyFallsBackToTheNameThePayloadUses(t *testing.T) {
	c := call{rows: "items", rowsAlt: "services"}
	if got := c.rowsKey(map[string]any{"services": []any{}}); got != "services" {
		t.Errorf("rowsKey = %q, want services", got)
	}
	if got := c.rowsKey(map[string]any{"items": []any{}}); got != rowsItems {
		t.Errorf("rowsKey = %q, want items", got)
	}
	if got := c.rowsKey(map[string]any{}); got != rowsItems {
		t.Errorf("rowsKey with neither key = %q, want the declared one", got)
	}
	if got := (call{}).rowsKey(map[string]any{}); got != "" {
		t.Errorf("rowsKey without a declared key = %q, want empty", got)
	}
}

func TestFormat(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{"text", "text"},
		{float64(5), "5"},
		{1.5, "1.5"},
		{7, "7"},
		{true, "true"},
		{[]string{"odd"}, "[odd]"},
	}
	for _, tc := range cases {
		if got := format(tc.in); got != tc.want {
			t.Errorf("format(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestAsListWrapsAScalar(t *testing.T) {
	if got := asList("one"); len(got) != 1 || got[0] != "one" {
		t.Errorf("asList = %v", got)
	}
	if got := asList([]any{1, 2}); len(got) != 2 {
		t.Errorf("asList = %v", got)
	}
}

func TestArgAccessors(t *testing.T) {
	args := map[string]any{"s": "x", "n": float64(4), "i": 9, "bad": true}
	if got := argString(args, "missing"); got != "" {
		t.Errorf("argString = %q", got)
	}
	if got := argStringOr(args, "missing", "fallback"); got != "fallback" {
		t.Errorf("argStringOr = %q", got)
	}
	if got := argStringOr(args, "s", "fallback"); got != "x" {
		t.Errorf("argStringOr = %q", got)
	}
	if got := argInt(args, "n", 1); got != 4 {
		t.Errorf("argInt(float) = %d", got)
	}
	if got := argInt(args, "i", 1); got != 9 {
		t.Errorf("argInt(int) = %d", got)
	}
	if got := argInt(args, "bad", 1); got != 1 {
		t.Errorf("argInt(wrong type) = %d, want the default", got)
	}
}

func TestUsageIsBoundedAndDefaultsToEveryProvider(t *testing.T) {
	rows := make([]any, maxPageSize+10)
	for i := range rows {
		rows[i] = map[string]any{"usage_type": fmt.Sprintf("t%d", i)}
	}
	f := &fakeFetcher{payloads: map[string]any{"/api/v1/costs/usage": map[string]any{"items": rows}}}
	got, err := toolNamed(t, "get-usage-costs").run(f, nil)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if got["returned"] != maxPageSize {
		t.Errorf("returned = %v, want %d", got["returned"], maxPageSize)
	}
	if !strings.Contains(f.paths[0], "provider=all") {
		t.Errorf("usage did not default to every provider: %q", f.paths[0])
	}
}
