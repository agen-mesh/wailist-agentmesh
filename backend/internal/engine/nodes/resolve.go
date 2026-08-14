package nodes

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// templateRef matches {{ ref }} with optional surrounding whitespace. The ref
// charset deliberately excludes braces and spaces so an unclosed or malformed
// reference simply fails to match and is left in the output verbatim.
var templateRef = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_.\-]+)\s*\}\}`)

// resolveTemplate expands {{ }} references in s against the run context:
//
//	{{ result }}            the most recent node output (rc.Message)
//	{{ input }}             the original trigger input (rc.UserInput)
//	{{ node.<id> }}         that node's output, stringified
//	{{ node.<id>.<field> }} one field of that node's object output
//
// An unresolvable reference is left verbatim. Blanking it would produce a
// silently empty Slack message or email body, which is harder to diagnose than
// a literal "{{ node.n7 }}" showing up in the output.
func resolveTemplate(s string, rc RunContexter) string {
	if s == "" || !strings.Contains(s, "{{") {
		return s
	}
	// FindAllStringSubmatchIndex captures both the full match and the ref
	// group's byte offsets in one pass, so each reference is matched exactly
	// once — ReplaceAllStringFunc's callback would otherwise re-run the same
	// regex against text the outer call already matched.
	matches := templateRef.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		start, end, refStart, refEnd := m[0], m[1], m[2], m[3]
		b.WriteString(s[last:start])
		if val, ok := lookupRef(s[refStart:refEnd], rc); ok {
			b.WriteString(val)
		} else {
			b.WriteString(s[start:end])
		}
		last = end
	}
	b.WriteString(s[last:])
	return b.String()
}

func lookupRef(ref string, rc RunContexter) (string, bool) {
	switch ref {
	case "result":
		return rc.Message(), true
	case "input":
		return rc.UserInput(), true
	}
	rest, isNode := strings.CutPrefix(ref, "node.")
	if !isNode || rest == "" {
		return "", false
	}
	nodeID, field, hasField := strings.Cut(rest, ".")
	out, ok := rc.Get(nodeID)
	if !ok {
		return "", false
	}
	if !hasField {
		return stringifyRef(out), true
	}
	obj, ok := out.(map[string]any)
	if !ok {
		return "", false
	}
	fieldVal, ok := obj[field]
	if !ok {
		return "", false
	}
	return stringifyRef(fieldVal), true
}

// stringifyRef renders a resolved value for interpolation into text. Strings
// pass through unquoted; numbers render without json.Marshal's float
// formatting surprises; everything else falls back to compact JSON.
func stringifyRef(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
