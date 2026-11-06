package models

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ParamFromSchema builds a ParamDef from one property of an x402 Bazaar
// input schema (JSON Schema) plus the catalog's example value for it.
//
// Everything a caller needs to send a value the endpoint accepts is kept:
// the schema's own description, its enum, string pattern/length and numeric
// bounds, and the example. Dropping these (as the importer once did, keeping
// only "example: ...") let an agent send e.g. domain_type="latest" or
// after_date="yesterday" and have the paid call rejected.
func ParamFromSchema(name string, schema map[string]any, example any, hasExample, required bool) ParamDef {
	p := ParamDef{Name: name, Type: "string", Required: required}
	if t, ok := schema["type"].(string); ok && t != "" {
		p.Type = t
	}
	var parts []string
	if d, ok := schema["description"].(string); ok && strings.TrimSpace(d) != "" {
		parts = append(parts, strings.TrimSpace(d))
	}
	if raw, ok := schema["enum"].([]any); ok && len(raw) > 0 {
		for _, v := range raw {
			p.Enum = append(p.Enum, fmt.Sprint(v))
		}
		parts = append(parts, "one of: "+strings.Join(p.Enum, ", "))
	}
	if pat, ok := schema["pattern"].(string); ok && pat != "" {
		if pat == `^\d{4}-\d{2}-\d{2}$` {
			parts = append(parts, "date as YYYY-MM-DD")
		} else {
			parts = append(parts, "must match "+pat)
		}
	}
	if f, ok := schema["format"].(string); ok && f != "" {
		parts = append(parts, "format "+f)
	}
	if b := bounds(schema, "minLength", "maxLength", "length"); b != "" {
		parts = append(parts, b)
	}
	if b := bounds(schema, "minimum", "maximum", "value"); b != "" {
		parts = append(parts, b)
	}
	if b := bounds(schema, "minItems", "maxItems", "items"); b != "" {
		parts = append(parts, b)
	}
	if hasExample {
		parts = append(parts, fmt.Sprintf("example: %v", example))
	}
	p.Description = strings.Join(parts, "; ")
	return p
}

func bounds(schema map[string]any, minKey, maxKey, what string) string {
	lo, hasLo := schema[minKey].(float64)
	hi, hasHi := schema[maxKey].(float64)
	// JSON Schema's "no limit" is often written as MAX_SAFE_INTEGER.
	if hasHi && hi >= 9007199254740991 {
		hasHi = false
	}
	n := func(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }
	switch {
	case hasLo && hasHi:
		return fmt.Sprintf("%s %s to %s", what, n(lo), n(hi))
	case hasLo:
		return fmt.Sprintf("%s at least %s", what, n(lo))
	case hasHi:
		return fmt.Sprintf("%s at most %s", what, n(hi))
	}
	return ""
}

// ParamsFromSchema turns a schema object ({properties, required}) and its
// example values into ParamDefs, sorted by name so a form built from them
// never reshuffles between two reads of the same endpoint. Names that
// appear only as examples (no schema property) are kept as plain strings.
func ParamsFromSchema(spec map[string]any, examples map[string]any) []ParamDef {
	props, _ := spec["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := spec["required"].([]any); ok {
		for _, r := range list {
			if n, ok := r.(string); ok {
				required[n] = true
			}
		}
	}
	seen := map[string]bool{}
	var names []string
	for _, src := range []map[string]any{props, examples} {
		for n := range src {
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	out := make([]ParamDef, 0, len(names))
	for _, n := range names {
		schema, _ := props[n].(map[string]any)
		ex, hasEx := examples[n]
		out = append(out, ParamFromSchema(n, schema, ex, hasEx, required[n]))
	}
	return out
}
