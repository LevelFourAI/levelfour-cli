package mcp

import (
	"encoding/json"
	"fmt"
)

// The result envelope is part of the contract: a model pages by reading
// has_next_page and next_page, so returning the REST layer's own pagination
// fields instead would stop a client that learned to page on the hosted server.

const (
	// maxPageSize is the caller-facing bound on a page, sized to stay inside the
	// smallest client result ceiling (Claude Code, 25,000 tokens).
	maxPageSize     = 50
	defaultPageSize = 10

	// maxRows bounds an unpaginated result after any caller-supplied limit.
	maxRows = 200

	// maxResultChars is the same token ceiling converted at four characters per
	// token, with room left for the client's own framing.
	maxResultChars = 60000
)

const (
	keyTotal      = "total"
	keyTotalCount = "total_count"
	keyPagination = "pagination"
)

func clampPageSize(size int) int {
	if size < 1 {
		return 1
	}
	if size > maxPageSize {
		return maxPageSize
	}
	return size
}

// total is nil for a service that does not count its rows; has_next_page then
// reports whether the page came back full, which is the only signal available.
// pageSpec is the page a caller asked for, and where its rows live.
type pageSpec struct {
	page     int
	size     int
	itemsKey string
}

func pageEnvelope(p pageSpec, total *int, returned int) map[string]any {
	envelope := map[string]any{
		argPage:     p.page,
		argPageSize: p.size,
		"returned":  returned,
	}
	hasNext := returned >= p.size
	if total != nil {
		envelope[keyTotal] = *total
		hasNext = p.page*p.size < *total
	}
	envelope["has_next_page"] = hasNext
	if hasNext {
		envelope["next_page"] = p.page + 1
	} else {
		envelope["next_page"] = nil
	}
	return envelope
}

// Stripped before the shared envelope is applied, so a payload never carries two
// competing answers to "is there another page".
var paginationKeys = map[string]bool{
	keyTotal: true, keyTotalCount: true, argPage: true, argPageSize: true,
	"pages": true, "total_pages": true, keyPagination: true,
	"has_next": true, "has_more": true,
}

func paginate(listing map[string]any, p pageSpec) map[string]any {
	rows, _ := listing[p.itemsKey].([]any)
	if rows == nil {
		rows = []any{}
	}
	shaped := map[string]any{}
	for k, v := range listing {
		if !paginationKeys[k] {
			shaped[k] = v
		}
	}
	shaped[p.itemsKey] = rows
	for k, v := range pageEnvelope(p, totalFrom(listing), len(rows)) {
		shaped[k] = v
	}
	return shaped
}

func totalFrom(listing map[string]any) *int {
	if n, ok := intField(listing); ok {
		return &n
	}
	if nested, ok := listing[keyPagination].(map[string]any); ok {
		if n, ok := intField(nested); ok {
			return &n
		}
	}
	return nil
}

func intField(m map[string]any) (int, bool) {
	for _, key := range []string{keyTotal, keyTotalCount} {
		// JSON numbers decode as float64, so an integer total arrives as one.
		if f, ok := m[key].(float64); ok && f == float64(int(f)) {
			return int(f), true
		}
	}
	return 0, false
}

// bounded caps an unpaginated service result before it reaches the model's
// context, and says it capped.
func bounded(rows []any, limit int, key string) map[string]any {
	bound := limit
	if bound < 1 {
		bound = 1
	}
	if bound > maxRows {
		bound = maxRows
	}
	capped := rows
	if len(capped) > bound {
		capped = capped[:bound]
	}
	result := map[string]any{key: capped, "returned": len(capped), keyTotal: len(rows)}
	if len(rows) > bound {
		result["truncated"] = fmt.Sprintf(
			"Showing the %d largest of %d. Narrow the date range or add a filter "+
				"rather than asking for more rows.", bound, len(rows))
	}
	return result
}

// hintIfEmpty attaches a next step to a result that came back with nothing. An
// empty list and a broken tool look identical to a model, and the usual recovery
// is to call the same tool again with the same arguments.
func hintIfEmpty(result map[string]any, rowsKey, hint string) map[string]any {
	rows, ok := result[rowsKey].([]any)
	if ok && len(rows) == 0 {
		result["hint"] = hint
	}
	return result
}

// fitBudget drops rows until the serialized result fits the budget, and says so.
// A model that gets a truncated answer can still act on it; one whose context was
// blown cannot.
func fitBudget(result map[string]any, rowsKey string) map[string]any {
	if sizeOf(result) <= maxResultChars {
		return result
	}
	rows, ok := result[rowsKey].([]any)
	if !ok || len(rows) == 0 {
		return result
	}

	kept := rows
	for len(kept) > 0 {
		trial := withRows(result, rowsKey, kept)
		if sizeOf(trial) <= maxResultChars {
			break
		}
		// Halving rather than stepping: a single oversized row would otherwise
		// cost one serialization per row to discover.
		kept = kept[:len(kept)/2]
	}

	trimmed := withRows(result, rowsKey, kept)
	trimmed["truncated"] = fmt.Sprintf(
		"Showing %d of %d rows because the full result exceeded the size a "+
			"single tool result may return. Ask a narrower question, or page through "+
			"with a smaller page_size.", len(kept), len(rows))
	return trimmed
}

func withRows(result map[string]any, rowsKey string, rows []any) map[string]any {
	out := make(map[string]any, len(result))
	for k, v := range result {
		out[k] = v
	}
	out[rowsKey] = rows
	return out
}

func sizeOf(payload any) int {
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Unmarshalable content cannot be measured, so treat it as over budget
		// and let the caller drop rows rather than shipping it unchecked.
		return maxResultChars + 1
	}
	return len(encoded)
}
