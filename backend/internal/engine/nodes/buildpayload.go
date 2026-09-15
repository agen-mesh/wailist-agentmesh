package nodes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// builderMaxOutputTokens bounds what one builder round may generate.
//
// Thinking tokens count toward this limit, and a round that reaches it
// mid-response comes back truncated or empty -- which, for a function call,
// means arguments cut off mid-object. So it sits far above anything a round
// legitimately produces (a tool call is small, a reply is a paragraph) and
// only stops a runaway, rather than being tuned tight to save a few tokens.
const builderMaxOutputTokens = 8192

// builderThinkingBudget, when positive, caps each builder round's thinking
// via generationConfig.thinkingConfig.thinkingBudget. Set once at startup
// from BUILDER_THINKING_BUDGET, mirroring platformKeysForTools rather than
// threading a process-wide value through BuildRequest.
//
// Zero sends no thinking config at all. The field has not been confirmed
// against generateContent on the builder's model, and one Gemini rejects
// fails every build, so it stays absent until someone turns it on and checks
// the per-round usage logs.
var builderThinkingBudget int

// SetBuilderThinkingBudget installs the per-round thinking cap. Zero or a
// negative value means unset.
func SetBuilderThinkingBudget(tokens int) {
	if tokens < 0 {
		tokens = 0
	}
	builderThinkingBudget = tokens
}

// buildPayload assembles the opening request of a build: the replayed
// history, the current turn carrying a fresh graph snapshot, the standing
// instructions, the graph tools and the generation limits.
//
// Prior turns come first and the snapshot rides with the newest turn on
// purpose: the graph changes between turns, and replaying an old one would
// leave the model reasoning about nodes that have since been renamed or
// removed.
func buildPayload(history []BuildTurn, graph models.WorkflowGraph, userMessage string) map[string]any {
	graphJSON, _ := json.Marshal(graph)
	contents := make([]map[string]any, 0, len(history)+1)
	for _, h := range history {
		// Gemini rejects any role other than user/model, and one bad row
		// replayed here would fail every build on this workflow from then on.
		if h.Role != "user" && h.Role != "model" {
			continue
		}
		// Clipped before the emptiness check, not after: a model turn that
		// was nothing but a test-run note clips to "", and Gemini rejects an
		// empty part as readily as it rejects a bad role.
		replayed := clipHistoryText(h.Role, h.Text)
		if strings.TrimSpace(replayed) == "" {
			continue
		}
		contents = append(contents, map[string]any{
			"role":  h.Role,
			"parts": []map[string]any{{"text": replayed}},
		})
	}
	contents = append(contents, map[string]any{
		"role":  "user",
		"parts": []map[string]any{{"text": fmt.Sprintf("Current graph:\n%s\n\nRequest: %s", graphJSON, userMessage)}},
	})

	generation := map[string]any{"maxOutputTokens": builderMaxOutputTokens}
	if builderThinkingBudget > 0 {
		generation["thinkingConfig"] = map[string]any{"thinkingBudget": builderThinkingBudget}
	}
	return map[string]any{
		"contents": contents,
		"systemInstruction": map[string]any{
			"parts": []map[string]string{{"text": buildSystemPrompt}},
		},
		"tools":            []map[string]any{{"functionDeclarations": graphToolDecls()}},
		"generationConfig": generation,
	}
}
