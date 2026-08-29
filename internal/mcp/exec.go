package mcp

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Fetcher performs one authenticated GET against the LevelFour REST API and
// returns the "data" member of the response envelope.
type Fetcher interface {
	Fetch(path string) (any, error)
}

// run executes a tool's REST plan and assembles the result.
//
// Arguments are not validated against the declared JSON Schema here. The REST
// API validates every one of these parameters and answers a bad value with a 422
// whose message names the accepted values, which is exactly what a model needs
// to correct itself. Validating twice would mean two sources of truth for what
// "sort_by" accepts, and the one in this binary would be the one that goes stale.
func (t tool) run(f Fetcher, args map[string]any) (map[string]any, error) {
	calls := t.calls
	if t.altArg != "" && argString(args, t.altArg) == "" {
		calls = t.altCalls
	}

	result := map[string]any{}
	for _, name := range t.echo {
		if value, ok := args[name]; ok {
			result[name] = value
		}
	}

	// The first call that yields rows, not the first call: get-realized-savings
	// carries its rows on the second one. A call with a key nests its payload, so
	// the section travels with the key or the budget looks at the wrong level.
	budget := rowsPath{key: rowsItems}
	foundRows := false
	for _, c := range calls {
		payload, err := c.fetch(f, args)
		if err != nil {
			return nil, err
		}

		shaped, rowsKey := c.shape(payload, args)
		if !foundRows {
			if path, ok := c.budgetPath(shaped, rowsKey); ok {
				budget, foundRows = path, true
			}
		}
		if hint, ok := c.hintForList(shaped); ok {
			result["hint"] = hint
		}
		if c.key == "" {
			result = merge(result, shaped)
			continue
		}
		result[c.key] = shaped
	}

	if t.shape != nil {
		result = t.shape(result)
	}
	return fitBudget(result, budget), nil
}

// fetch issues the call and narrows the payload to the part it names.
func (c call) fetch(f Fetcher, args map[string]any) (any, error) {
	payload, err := f.Fetch(c.request(args))
	if err != nil || c.pick == "" {
		return payload, err
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return payload, nil
	}
	return object[c.pick], nil
}

// hintForList is the empty-list hint for a picked list. hintIfEmpty writes into
// an object, and a list is not one, so a hint declared on a call that picks a
// list reached nobody. An empty list and a broken tool look the same to a model.
func (c call) hintForList(shaped any) (string, bool) {
	rows, isList := shaped.([]any)
	if !isList || c.hint == "" || len(rows) > 0 {
		return "", false
	}
	return c.hint, true
}

// request renders the REST path and query string for one call.
func (c call) request(args map[string]any) string {
	path := c.path
	if strings.Contains(path, "{provider}") {
		path = strings.ReplaceAll(path, "{provider}", url.PathEscape(argStringOr(args, argProvider, defaultAWS)))
	}
	if strings.Contains(path, "{recommendation_id}") {
		id := url.PathEscape(argString(args, "recommendation_id"))
		path = strings.ReplaceAll(path, "{recommendation_id}", id)
	}

	query := url.Values{}
	for _, b := range c.query {
		b.apply(query, args)
	}
	if len(query) == 0 {
		return path
	}
	return path + "?" + query.Encode()
}

func (b binding) apply(query url.Values, args map[string]any) {
	value, present := args[b.arg]
	if !present || value == nil {
		if b.def != nil {
			query.Set(b.param, format(b.def))
		}
		return
	}
	if b.list {
		for _, item := range asList(value) {
			query.Add(b.param, format(item))
		}
		return
	}
	query.Set(b.param, format(value))
}

// shape applies the result rules a call declares, and reports which key its rows
// landed under so the size budget knows what it may trim.
// budgetPath is where this call's rows end up in the assembled result, which
// depends on what the call returned. An object nests under the call's key, so the
// key travels with the rows key. A pick can hand back a bare list, which sits
// directly under the call's key with no object around it; addressing that as an
// object found nothing, and the size ceiling silently never applied.
func (c call) budgetPath(shaped any, rowsKey string) (rowsPath, bool) {
	if _, isList := shaped.([]any); isList && c.key != "" {
		return rowsPath{key: c.key}, true
	}
	if rowsKey == "" {
		return rowsPath{}, false
	}
	return rowsPath{section: c.key, key: rowsKey}, true
}

func (c call) shape(payload any, args map[string]any) (any, string) {
	// A picked list still earns its cap: the route returns it whole.
	if rows, isList := payload.([]any); isList {
		if c.bound && len(rows) > maxPageSize {
			return rows[:maxPageSize], ""
		}
		return rows, ""
	}
	object, ok := payload.(map[string]any)
	if !ok {
		return payload, ""
	}

	if c.paged {
		page := argInt(args, argPage, 1)
		size := clampPageSize(argInt(args, argPageSize, defaultPageSize))
		object = paginate(object, pageSpec{page: page, size: size, itemsKey: c.rows})
	}

	rowsKey := c.rowsKey(object)
	if rowsKey == "" {
		return object, ""
	}
	if c.bound {
		if rows, ok := object[rowsKey].([]any); ok {
			object = merge(object, bounded(rows, maxPageSize, rowsKey))
		}
	}
	if c.hint != "" {
		object = hintIfEmpty(object, rowsKey, c.hint)
	}
	return object, rowsKey
}

// rowsKey picks the items key actually present in the payload. Two REST routes
// name the same list differently depending on the shape they return, and a hint
// attached to a key that is not there would never be seen.
func (c call) rowsKey(object map[string]any) string {
	if c.rows == "" {
		return ""
	}
	if _, ok := object[c.rows]; ok {
		return c.rows
	}
	if c.rowsAlt != "" {
		if _, ok := object[c.rowsAlt]; ok {
			return c.rowsAlt
		}
	}
	return c.rows
}

func merge(into map[string]any, from any) map[string]any {
	object, ok := from.(map[string]any)
	if !ok {
		// A REST route that answers with a list rather than an object still has
		// to reach the caller, so give it a name instead of dropping it.
		into["data"] = from
		return into
	}
	for k, v := range object {
		into[k] = v
	}
	return into
}

func format(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}

func argString(args map[string]any, name string) string {
	s, _ := args[name].(string)
	return s
}

func argStringOr(args map[string]any, name, def string) string {
	if s := argString(args, name); s != "" {
		return s
	}
	return def
}

func argInt(args map[string]any, name string, def int) int {
	switch v := args[name].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return def
	}
}
