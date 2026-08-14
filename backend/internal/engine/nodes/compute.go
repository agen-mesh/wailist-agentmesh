package nodes

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// executeSet builds an object from the node's `setFields` JSON, expanding
// {{ }} references in every string value. It returns a map rather than a
// string so downstream nodes can address individual fields with
// {{ node.<id>.<field> }}.
func executeSet(node models.WorkflowNode, rc RunContexter) (any, error) {
	raw := configVal(node, "setFields", "")
	if raw == "" {
		return nil, errors.New("set: no fields configured — set `setFields` to a JSON object")
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return nil, fmt.Errorf("set: `setFields` is not a valid JSON object: %w", err)
	}
	out := make(map[string]any, len(fields))
	for k, v := range fields {
		if s, ok := v.(string); ok {
			out[k] = resolveTemplate(s, rc)
			continue
		}
		out[k] = v
	}
	return out, nil
}

// executeJSONExtract parses the upstream output as JSON and walks a dot path
// into it. Numeric segments index arrays: "data.items.0.name".
func executeJSONExtract(node models.WorkflowNode, rc RunContexter) (any, error) {
	path := configVal(node, "jsonPath", "")
	if path == "" {
		return nil, errors.New("json_extract: no `jsonPath` configured")
	}
	var doc any
	if err := json.Unmarshal([]byte(rc.Message()), &doc); err != nil {
		return nil, fmt.Errorf("json_extract: upstream output is not valid JSON: %w", err)
	}
	cur := doc
	for _, seg := range strings.Split(path, ".") {
		switch node := cur.(type) {
		case map[string]any:
			v, ok := node[seg]
			if !ok {
				return nil, fmt.Errorf("json_extract: no value at path %q (missing key %q)", path, seg)
			}
			cur = v
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil {
				return nil, fmt.Errorf("json_extract: path %q indexes an array with non-numeric segment %q", path, seg)
			}
			if i < 0 || i >= len(node) {
				return nil, fmt.Errorf("json_extract: index %d out of range at path %q (length %d)", i, path, len(node))
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("json_extract: path %q descends past a scalar at %q", path, seg)
		}
	}
	return cur, nil
}
