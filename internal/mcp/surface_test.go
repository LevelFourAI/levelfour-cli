package mcp

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// hostedCatalog is the hosted server's own generated tool snapshot, vendored.
// testdata/hosted_catalog.provenance.json records what it was derived from and
// the sha256 of both sides, and TestVendoredCatalogIsIntact checks the hash, so
// editing this file to make a failure go away fails louder than the failure it
// was hiding.
//
// It is derived rather than verbatim. Tools this surface never serves keep their
// name and annotation, which is all the gate needs to refuse them, and drop
// their description and schema: this repository is public and does not ship
// them.
//
// Nothing yet checks the copy against a moving upstream. That job belongs on the
// API side, because only it knows when its surface changed and only it can read
// this public repository without a secret. Until it exists, this file is as
// fresh as the last person made it, and saying so here beats a comment implying
// CI covers it.
type hostedCatalog struct {
	Tools []hostedTool `json:"tools"`
}

type hostedTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Annotations struct {
		ReadOnlyHint *bool `json:"read_only_hint"`
	} `json:"annotations"`
}

// writes reports whether the hosted server treats this tool as a write. The
// annotation is the authority: the hosted server hides exactly these from a
// read-only credential, so it is also what decides whether this surface owes
// the tool.
func (h hostedTool) writes() bool {
	return h.Annotations.ReadOnlyHint != nil && !*h.Annotations.ReadOnlyHint
}

func loadHostedCatalog(t *testing.T) hostedCatalog {
	t.Helper()
	raw, err := os.ReadFile("testdata/hosted_catalog.json")
	if err != nil {
		t.Fatalf("reading hosted catalog: %v", err)
	}
	var catalog hostedCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatalf("parsing hosted catalog: %v", err)
	}
	if len(catalog.Tools) == 0 {
		t.Fatal("hosted catalog is empty")
	}
	return catalog
}

// partition splits the catalog the way the hosted server splits it per caller.
func (c hostedCatalog) partition() (read, write map[string]hostedTool) {
	read, write = map[string]hostedTool{}, map[string]hostedTool{}
	for _, tool := range c.Tools {
		if tool.writes() {
			write[tool.Name] = tool
			continue
		}
		read[tool.Name] = tool
	}
	return read, write
}

// TestVendoredCatalogIsIntact hashes the copy against the provenance file. A
// hash does not make the copy fresh, but it does make editing it a deliberate
// act rather than a quiet one, which a hand-maintained list of names never was.
func TestVendoredCatalogIsIntact(t *testing.T) {
	raw, err := os.ReadFile("testdata/hosted_catalog.json")
	if err != nil {
		t.Fatalf("reading hosted catalog: %v", err)
	}
	meta, err := os.ReadFile("testdata/hosted_catalog.provenance.json")
	if err != nil {
		t.Fatalf("reading provenance: %v", err)
	}
	var provenance struct {
		SHA256   string `json:"sha256"`
		Upstream string `json:"upstream_sha256"`
	}
	if err := json.Unmarshal(meta, &provenance); err != nil {
		t.Fatalf("parsing provenance: %v", err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(raw)); got != provenance.SHA256 {
		t.Errorf("hosted_catalog.json hashes to %s, provenance records %s.\n"+
			"Regenerate it from the hosted server's tool snapshot and update the "+
			"provenance file, rather than editing the copy in place", got, provenance.SHA256)
	}
	// The hash of the upstream snapshot, not a branch or a commit id. It is the
	// anchor that says which generated catalog this was derived from, and unlike
	// an internal revision it means nothing to a reader of this public repository.
	if provenance.Upstream == "" {
		t.Error("provenance records no upstream hash, so there is no way to tell what this was derived from")
	}
}

// TestSurfaceMatchesTheCatalogForThisCredential is the drift gate.
//
// The hosted catalog is not one list. The server computes it per caller: a
// read-write credential is shown all eighteen tools, a read credential the
// sixteen whose read_only_hint annotation is not false.
// This server holds exactly one credential and reaches everything over REST with
// a GET-only Fetcher, so the catalog it has to match is the read one.
//
// Both directions below are derived from read_only_hint in the vendored
// snapshot, so there is still no exception list to escape into: a hosted read
// tool this surface leaves out fails, and so does a name this surface invents.
// The exception is the hosted server's own annotation, and it travels in the
// file rather than being restated here.
//
// A write tool is therefore not a gap to declare, it is a question this gate
// answers on its own. Serving one here would show a model a tool it cannot
// call: every credential `l4 mcp install` mints is read scoped, and the local
// Fetcher issues GET only. Whether either of them should ever be served from
// this binary is a question about the API rather than about this surface, and
// it is tracked there.
//
// What this gate cannot see: the hosted server calls its services directly and
// makes no REST calls, so the snapshot describes neither the routes this surface
// calls nor the payload keys it reads. Every rows-key, pagination and byte-budget
// defect lives in that gap, and a green run here is not parity on its own.
func TestSurfaceMatchesTheCatalogForThisCredential(t *testing.T) {
	read, write := loadHostedCatalog(t).partition()

	// Literals, so a re-partition upstream is a deliberate edit here rather than
	// a silent widening of what this surface is allowed to omit.
	if len(read) != 16 || len(write) != 2 {
		t.Errorf("hosted catalog partitioned %d read / %d write, want 16 / 2", len(read), len(write))
	}

	local := map[string]bool{}
	for _, name := range ToolNames() {
		if local[name] {
			t.Errorf("tool %q is registered twice", name)
		}
		local[name] = true

		if _, ok := write[name]; ok {
			t.Errorf("tool %q writes on the hosted server and must not be served here: the local "+
				"Fetcher is GET-only, and every credential `l4 mcp install` mints is read-scoped, "+
				"so a model would be shown a tool it cannot call", name)
			continue
		}
		if _, ok := read[name]; !ok {
			t.Errorf("tool %q is served locally but is not in the hosted catalog", name)
		}
	}

	for name := range read {
		if !local[name] {
			t.Errorf("hosted read tool %q is not served locally, and this surface carries every one "+
				"of them. A read tool that will not port is a REST route to open, not a gap to declare", name)
		}
	}
}

func TestSchemasMatchPydanticShape(t *testing.T) {
	for _, tl := range tools {
		var schema map[string]any
		if err := json.Unmarshal(tl.schema, &schema); err != nil {
			t.Fatalf("%s: schema is not JSON: %v", tl.name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("%s: schema type is %v, want object", tl.name, schema["type"])
		}
		// The hosted schema title is the wire name with underscores, which is what
		// the vendored catalog carries for every tool.
		want := strings.ReplaceAll(tl.name, "-", "_") + "Arguments"
		if schema["title"] != want {
			t.Errorf("%s: schema title is %v, want %q", tl.name, schema["title"], want)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: schema has no properties object", tl.name)
		}
		for name, value := range properties {
			body, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("%s.%s: property is not an object", tl.name, name)
			}
			if body["title"] == nil {
				t.Errorf("%s.%s: property has no title, which Pydantic always emits", tl.name, name)
			}
			if body["description"] == nil {
				t.Errorf("%s.%s: property has no description", tl.name, name)
			}
		}
	}
}

// TestPagingBoundsMatchThePydanticFields pins the one asymmetry that is easy to
// get wrong: page_size and limit are declared le=MAX_PAGE_SIZE on the hosted
// server, page is not. Giving page a ceiling here would reject an argument the
// hosted server accepts.
func TestPagingBoundsMatchThePydanticFields(t *testing.T) {
	for _, tl := range tools {
		var schema map[string]any
		if err := json.Unmarshal(tl.schema, &schema); err != nil {
			t.Fatal(err)
		}
		properties, _ := schema["properties"].(map[string]any)
		for name, value := range properties {
			body := value.(map[string]any)
			_, bounded := body["maximum"]
			switch name {
			case argPage:
				if bounded {
					t.Errorf("%s.page declares a maximum the hosted schema does not have", tl.name)
				}
			case argPageSize, argLimit:
				if !bounded || body["maximum"] != float64(maxPageSize) {
					t.Errorf("%s.%s maximum = %v, want %d", tl.name, name, body["maximum"], maxPageSize)
				}
			}
		}
	}
}

func TestDescriptionsCarryRoutingGuidance(t *testing.T) {
	for _, tl := range tools {
		if !strings.HasPrefix(tl.description, "**Purpose:**") {
			t.Errorf("%s: description does not open with the Purpose line", tl.name)
		}
		if !strings.Contains(tl.description, "**NOT for:**") {
			t.Errorf("%s: description has no NOT for line, which is what stops a confusable neighbour", tl.name)
		}
		if strings.Contains(tl.description, emDash) {
			t.Errorf("%s: description contains an em-dash", tl.name)
		}
	}
}

// TestCrossReferencesResolve catches model-facing prose pointing at a tool name
// that does not exist anywhere, which would send a model to a dead end.
//
// Prompt bodies are scanned as well as tool descriptions. A prompt is the one
// place a model is handed a routing plan several tools long, so a stale name
// there costs more than the same name in a description, and the version of this
// test that walked only `tools` could not see them.
//
// Locally served names are checked separately from hosted ones. A reference to a
// hosted write tool resolves for a model on the hosted server and dead-ends here,
// so it is called out rather than passed.
func TestCrossReferencesResolve(t *testing.T) {
	read, write := loadHostedCatalog(t).partition()
	local := map[string]bool{}
	for _, name := range ToolNames() {
		local[name] = true
	}

	check := func(where, prose string) {
		for _, word := range strings.Fields(strings.ReplaceAll(prose, "`", " ")) {
			name := strings.Trim(word, ".,\"'();:")
			// Wire names are verb-first kebab with no vendor prefix, so this is what
			// marks a cross-reference in prose.
			if !strings.HasPrefix(name, "get-") && !strings.HasPrefix(name, "list-") &&
				!strings.HasPrefix(name, "decide-") && !strings.HasPrefix(name, "update-") {
				continue
			}
			if _, ok := write[name]; ok {
				t.Errorf("%s references %q, which this surface does not serve. A model reading this "+
					"has no way to reach it: name the local substitute instead", where, name)
				continue
			}
			if _, ok := read[name]; !ok {
				t.Errorf("%s references %q, which is not in the hosted catalog", where, name)
				continue
			}
			if !local[name] {
				t.Errorf("%s references %q, which is in the hosted catalog but not served here", where, name)
			}
		}
	}

	for _, tl := range tools {
		check(tl.name, tl.description)
	}
	for _, p := range prompts {
		check("prompt "+p.name, p.description+" "+p.render(nil))
	}
}

func TestTitle(t *testing.T) {
	cases := map[string]string{
		"page_size":         "Page Size",
		"recommendation_id": "Recommendation Id",
		"provider":          "Provider",
		"":                  "",
		"_leading":          " Leading",
	}
	for in, want := range cases {
		if got := title(in); got != want {
			t.Errorf("title(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestObjectMarksRequired(t *testing.T) {
	raw := object("xArguments", patternProp("id", "an id", "^x$"), optStringProp("other", "optional"))
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	required, ok := schema["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "id" {
		t.Errorf("required = %v, want [id]", schema["required"])
	}
}

func TestObjectOmitsRequiredWhenNoneAre(t *testing.T) {
	var schema map[string]any
	if err := json.Unmarshal(object("emptyArguments"), &schema); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := schema["required"]; present {
		t.Error("schema with no required properties should omit the required key")
	}
}

func TestSchemaBuilders(t *testing.T) {
	cases := []struct {
		name string
		p    prop
		want string
	}{
		{"int", intProp("page", "d", 1), `"minimum":1`},
		{"boundedInt", boundedIntProp("page_size", "d", 10), `"maximum":50`},
		{"enum", enumProp("sort_order", "d", "desc", "asc", "desc"), `"enum":["asc","desc"]`},
		{"optEnum", optEnumProp("status", "d", "active"), `"anyOf":[{"enum":["active"],"type":"string"}`},
		{"optDate", optDateProp("start", "d"), `"format":"date"`},
		{"optList", optListProp("service", "d"), `"type":"array"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			encoded, err := json.Marshal(tc.p.body)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !strings.Contains(string(encoded), tc.want) {
				t.Errorf("%s missing %s: %s", tc.name, tc.want, encoded)
			}
		})
	}
}

// emDash is spelled as an escape so the character itself never appears in this
// repository, which is the rule these assertions exist to enforce.
const emDash = "\u2014"
