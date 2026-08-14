package nodes

import (
	"encoding/json"
	"errors"
	"fmt"

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
