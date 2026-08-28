package mcp

import (
	"encoding/json"
	"strings"
)

// JSON Schema fragments are built here rather than written out as literals so the
// shapes stay identical across sixteen tools. The hosted server derives its schemas
// from Pydantic models, and these builders reproduce what Pydantic emits: a "title"
// per field, an anyOf against null for an optional, and the default carried in the
// schema rather than only in the description. A client that renders an argument form
// from the schema then renders the same form against either surface.

// The JSON Schema vocabulary these builders emit, named once so a typo in one
// builder cannot produce a fragment that differs from its neighbours.
const (
	kwType        = "type"
	kwTitle       = "title"
	kwDescription = "description"
	kwDefault     = "default"
	kwEnum        = "enum"
	kwAnyOf       = "anyOf"

	typeObject  = "object"
	typeString  = "string"
	typeInteger = "integer"
	typeArray   = "array"
	typeNull    = "null"
)

type prop struct {
	name     string
	body     map[string]any
	required bool
}

// title turns a field name into the title Pydantic derives from it, so "page_size"
// becomes "Page Size" and "recommendation_id" becomes "Recommendation Id".
func title(name string) string {
	words := strings.Split(name, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// object assembles the argument schema for one tool. objectTitle is the Pydantic
// model name the hosted server generates, "<function>Arguments".
func object(objectTitle string, props ...prop) json.RawMessage {
	properties := map[string]any{}
	var required []string
	for _, p := range props {
		p.body[kwTitle] = title(p.name)
		properties[p.name] = p.body
		if p.required {
			required = append(required, p.name)
		}
	}
	schema := map[string]any{
		"properties": properties,
		kwTitle:      objectTitle,
		kwType:       typeObject,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	raw, _ := json.Marshal(schema)
	return raw
}

// intProp mirrors a Pydantic int field declared ge=1 with no upper bound. Only
// "page" is one: a caller may ask for page 900, and the answer is an empty page,
// not a validation error.
func intProp(name, description string, def int) prop {
	return prop{name: name, body: map[string]any{
		kwDefault:     def,
		kwDescription: description,
		"minimum":     1,
		kwType:        typeInteger,
	}}
}

// boundedIntProp mirrors a Pydantic int field declared ge=1, le=MAX_PAGE_SIZE.
// The ceiling is in the schema rather than only in clampPageSize so a client can
// see the bound before it sends a request it will not get.
func boundedIntProp(name, description string, def int) prop {
	body := intProp(name, description, def)
	body.body["maximum"] = maxPageSize
	return body
}

func enumProp(name, description, def string, values ...string) prop {
	return prop{name: name, body: map[string]any{
		kwDefault:     def,
		kwDescription: description,
		kwEnum:        values,
		kwType:        typeString,
	}}
}

// nullable wraps a type fragment the way Pydantic renders "T | None = None".
func nullable(name, description string, fragment map[string]any) prop {
	return prop{name: name, body: map[string]any{
		kwAnyOf:       []any{fragment, map[string]any{kwType: typeNull}},
		kwDefault:     nil,
		kwDescription: description,
	}}
}

func optStringProp(name, description string) prop {
	return nullable(name, description, map[string]any{kwType: typeString})
}

func optDateProp(name, description string) prop {
	return nullable(name, description, map[string]any{"format": "date", kwType: typeString})
}

func optListProp(name, description string) prop {
	return nullable(name, description, map[string]any{"items": map[string]any{kwType: typeString}, kwType: typeArray})
}

func optEnumProp(name, description string, values ...string) prop {
	return nullable(name, description, map[string]any{kwEnum: values, kwType: typeString})
}

func patternProp(name, description, pattern string) prop {
	return prop{name: name, required: true, body: map[string]any{
		kwDescription: description,
		"pattern":     pattern,
		kwType:        typeString,
	}}
}
