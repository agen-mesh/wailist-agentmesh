package nodes

import (
	"strings"
	"testing"
)

func TestBuildPayloadAlwaysBoundsOutput(t *testing.T) {
	t.Cleanup(func() { SetBuilderThinkingBudget(0) })
	SetBuilderThinkingBudget(0)

	p := buildPayload(nil, wiredAgentGraph(), "add a step")
	gen, ok := p["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("no generationConfig on the build payload, so one runaway reply is unbounded")
	}
	if gen["maxOutputTokens"] != builderMaxOutputTokens {
		t.Errorf("maxOutputTokens = %v, want %d", gen["maxOutputTokens"], builderMaxOutputTokens)
	}
	// Unset means absent, not zero. The field has not been verified against
	// generateContent on this model, and a rejected field fails every build.
	if _, has := gen["thinkingConfig"]; has {
		t.Error("thinkingConfig sent while no budget is configured")
	}
}

func TestBuildPayloadSendsThinkingBudgetOnlyWhenSet(t *testing.T) {
	t.Cleanup(func() { SetBuilderThinkingBudget(0) })
	tests := []struct {
		name   string
		budget int
		want   bool
	}{
		{"a positive budget is sent", 1024, true},
		{"zero is unset", 0, false},
		{"a negative budget is unset", -5, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetBuilderThinkingBudget(tt.budget)
			gen := buildPayload(nil, wiredAgentGraph(), "add a step")["generationConfig"].(map[string]any)
			tc, has := gen["thinkingConfig"].(map[string]any)
			if has != tt.want {
				t.Fatalf("thinkingConfig present = %v, want %v", has, tt.want)
			}
			if tt.want && tc["thinkingBudget"] != tt.budget {
				t.Errorf("thinkingBudget = %v, want %d", tc["thinkingBudget"], tt.budget)
			}
		})
	}
}

// TestBuildPayloadKeepsWhatTheRequestCarried guards the extraction itself:
// pulling the payload out of BuildGraph must not change what reaches Gemini.
func TestBuildPayloadKeepsWhatTheRequestCarried(t *testing.T) {
	history := []BuildTurn{
		{Role: "user", Text: "build a price workflow"},
		{Role: "model", Text: "Built it."},
		{Role: "system", Text: "an invalid role is dropped"},
		{Role: "user", Text: "   "},
	}
	p := buildPayload(history, wiredAgentGraph(), "add a slack step")

	contents, ok := p["contents"].([]map[string]any)
	if !ok {
		t.Fatalf("contents is %T, want []map[string]any", p["contents"])
	}
	// Two replayable turns plus the current one; the bad role and the blank
	// turn are dropped exactly as before.
	if len(contents) != 3 {
		t.Fatalf("contents has %d turns, want 3", len(contents))
	}
	last := contents[len(contents)-1]
	if last["role"] != "user" {
		t.Errorf("the current turn has role %v, want user", last["role"])
	}
	parts := last["parts"].([]map[string]any)
	current, _ := parts[0]["text"].(string)
	if !strings.Contains(current, "Current graph:") || !strings.Contains(current, "Request: add a slack step") {
		t.Errorf("the current turn lost the graph snapshot or the request: %q", current)
	}
	if _, ok := p["systemInstruction"]; !ok {
		t.Error("systemInstruction is missing")
	}
	if _, ok := p["tools"]; !ok {
		t.Error("tools are missing")
	}
}
