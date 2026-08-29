package mcp

import (
	"strings"
	"testing"
)

func oversizedRows(n int) []any {
	rows := make([]any, n)
	for i := range rows {
		rows[i] = map[string]any{"a": strings.Repeat("x", 4000), "b": strings.Repeat("y", 4000)}
	}
	return rows
}

// A tool whose call carries a key nests its rows one level down. Trimming used
// to look only at the top level, so these returned whole and unannounced: a
// 50-row page of get-costs-by-tag serialized to 400KB against a 60KB ceiling.
func TestBudgetTrimsRowsNestedUnderACallKey(t *testing.T) {
	for _, section := range []string{"by_tag", "anomalies", "breakdown"} {
		t.Run(section, func(t *testing.T) {
			result := map[string]any{section: map[string]any{rowsItems: oversizedRows(50)}}
			got := fitBudget(result, rowsPath{section: section, key: rowsItems})

			if size := sizeOf(got); size > maxResultChars {
				t.Errorf("result is %d chars, over the %d ceiling", size, maxResultChars)
			}
			if got["truncated"] == nil {
				t.Error("rows were dropped without telling the model")
			}
			if _, untouched := got[rowsItems]; untouched {
				t.Error("trimming wrote rows to the top level instead of into the section")
			}
		})
	}
}

// The trim must not reach into the caller's map, or a retry sees dropped rows.
func TestBudgetDoesNotMutateTheOriginal(t *testing.T) {
	rows := oversizedRows(50)
	result := map[string]any{"by_tag": map[string]any{rowsItems: rows}}
	fitBudget(result, rowsPath{section: "by_tag", key: rowsItems})

	still := result["by_tag"].(map[string]any)[rowsItems].([]any)
	if len(still) != 50 {
		t.Errorf("original now holds %d rows, want 50", len(still))
	}
}

// The catalog-wide gate, driven through the real executor rather than a
// hand-written path, which is what let the nested case go unnoticed.
//
// Each route is served rows under its own key only. Serving every key a call
// declares would be the unrealistic case: rows and rowsAlt are alternatives, and
// a summary route returns no rows at all.
func TestEveryToolStaysInsideTheSizeCeiling(t *testing.T) {
	for _, tl := range tools {
		t.Run(tl.name, func(t *testing.T) {
			// A tool with an altArg answers on two different call sets, and the one
			// reached by empty arguments is the alternate. Both are checked, or the
			// main calls of get-costs-by-tag are never run.
			for _, args := range argSets(tl) {
				payloads, serves := map[string]any{}, false
				for _, calls := range [][]call{tl.calls, tl.altCalls} {
					for _, c := range calls {
						body := map[string]any{}
						if key := firstKey(c.rows, c.rowsAlt); key != "" {
							body[key] = oversizedRows(50)
							serves = true
						}
						payloads[basePath(c.request(args))] = body
					}
				}
				if !serves {
					t.Skip("tool returns no rows")
				}

				got, err := tl.run(&fakeFetcher{payloads: payloads}, args)
				if err != nil {
					t.Fatalf("run(%v): %v", args, err)
				}
				if size := sizeOf(got); size > maxResultChars {
					t.Errorf("run(%v): result is %d chars, over the %d ceiling, and nothing trimmed it",
						args, size, maxResultChars)
				}
			}
		})
	}
}

// argSets returns the argument sets that reach every branch of a tool.
func argSets(tl tool) []map[string]any {
	if tl.altArg == "" {
		return []map[string]any{{}}
	}
	return []map[string]any{{}, {tl.altArg: "any-value"}}
}

func firstKey(keys ...string) string {
	for _, k := range keys {
		if k != "" {
			return k
		}
	}
	return ""
}

func basePath(path string) string {
	if i := strings.Index(path, "?"); i >= 0 {
		return path[:i]
	}
	return path
}
