package nodes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agentmesh/backend/internal/models"
)

// builderReplyTokens is the headroom a round needs for what it actually
// returns: a function call (small, a name and a handful of arguments) or a
// reply to the user (a paragraph or two). Generous against both.
const builderReplyTokens = 8192

// maxGeminiThinkingTokens is the largest thinking budget gemini-2.5-flash
// accepts, and so the most it can spend when thinking is left dynamic. The
// cap below has to clear it: thinking tokens count toward maxOutputTokens,
// so a cap lower than this would be reached mid-thought on exactly the hard
// rounds, and a round that reaches it comes back with neither text nor a
// function call.
const maxGeminiThinkingTokens = 24576

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

// builderMaxOutputTokens bounds what one builder round may generate.
//
// It is derived from the thinking budget rather than fixed, because thinking
// tokens count toward this same limit. A round that reaches the limit
// mid-response comes back truncated or empty, the loop re-posts an identical
// payload, and both retries fail the same way -- so a cap set below what
// thinking legitimately spends does not save money, it fails the build.
//
// With no budget set, thinking is dynamic and can reach
// maxGeminiThinkingTokens, so the cap clears that and leaves room to answer.
// With a budget set, thinking cannot exceed it, so the cap only has to clear
// the budget. Either way this stops a runaway and nothing else; the saving
// comes from the budget, never from the cap.
func builderMaxOutputTokens() int {
	if builderThinkingBudget > 0 {
		return builderThinkingBudget + builderReplyTokens
	}
	return maxGeminiThinkingTokens + builderReplyTokens
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

	generation := map[string]any{"maxOutputTokens": builderMaxOutputTokens()}
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
