package nodes

import (
	"strings"
	"testing"
)

func TestBuildPayloadAlwaysBoundsOutput(t *testing.T) {
	t.Cleanup(ClearBuilderThinkingBudget)
	ClearBuilderThinkingBudget()

	p := buildPayload(nil, wiredAgentGraph(), "add a step")
	gen, ok := p["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("no generationConfig on the build payload, so one runaway reply is unbounded")
	}
	if gen["maxOutputTokens"] != builderMaxOutputTokens() {
		t.Errorf("maxOutputTokens = %v, want %d", gen["maxOutputTokens"], builderMaxOutputTokens())
	}
	// Unset means absent, not zero. The field has not been verified against
	// generateContent on this model, and a rejected field fails every build.
	if _, has := gen["thinkingConfig"]; has {
		t.Error("thinkingConfig sent while no budget is configured")
	}
}

func TestBuildPayloadSendsThinkingBudgetOnlyWhenSet(t *testing.T) {
	t.Cleanup(ClearBuilderThinkingBudget)
	tests := []struct {
		name   string
		budget int
		want   bool
	}{
		{"a positive budget is sent", 1024, true},
		// Gemini reads thinkingBudget 0 as thinking OFF. Treating it as
		// "unset" left thinking uncapped while the operator who set it
		// believed it was disabled.
		{"zero turns thinking off and is sent", 0, true},
		{"a negative budget is not a budget, so nothing is sent", -5, false},
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

// The cap includes thinking tokens, so a cap at or below what thinking can
// spend is reached mid-thought and the round comes back unusable -- on
// exactly the hard rounds a build cannot afford to lose. Every configuration
// must leave room to think AND to answer.
func TestOutputCapAlwaysLeavesRoomToAnswerAfterThinking(t *testing.T) {
	t.Cleanup(ClearBuilderThinkingBudget)
	tests := []struct {
		name          string
		budget        int
		thinkingSpend int
	}{
		{"thinking off spends nothing", 0, 0},
		{"a small budget", 1024, 1024},
		{"a budget at the model's ceiling", maxGeminiThinkingTokens, maxGeminiThinkingTokens},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetBuilderThinkingBudget(tt.budget)
			cap := builderMaxOutputTokens()
			if cap <= tt.thinkingSpend {
				t.Fatalf("cap %d does not exceed the %d tokens thinking may spend: a round that thinks that hard returns nothing, and the retry re-sends the same payload", cap, tt.thinkingSpend)
			}
			if room := cap - tt.thinkingSpend; room < builderReplyTokens {
				t.Errorf("only %d tokens left to answer in after thinking, want at least %d", room, builderReplyTokens)
			}
		})
	}
}

func TestFinishedOnOutputLimitReadsTheFinishReason(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want bool
	}{
		{
			name: "truncated by the output limit",
			resp: map[string]any{"candidates": []any{map[string]any{"finishReason": "MAX_TOKENS"}}},
			want: true,
		},
		{
			name: "a normal finish",
			resp: map[string]any{"candidates": []any{map[string]any{"finishReason": "STOP"}}},
			want: false,
		},
		{
			name: "no candidates at all is a different failure, handled elsewhere",
			resp: map[string]any{},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FinishedOnOutputLimit(tt.resp); got != tt.want {
				t.Errorf("FinishedOnOutputLimit = %v, want %v", got, tt.want)
			}
		})
	}
}

// Unset is not the same as zero, and the difference is the whole point: with
// no budget, thinking is dynamic and can reach the model's ceiling, so the
// cap has to clear that ceiling.
func TestOutputCapClearsDynamicThinkingWhenNoBudgetIsSet(t *testing.T) {
	t.Cleanup(ClearBuilderThinkingBudget)
	ClearBuilderThinkingBudget()
	if cap := builderMaxOutputTokens(); cap <= maxGeminiThinkingTokens {
		t.Fatalf("cap %d does not clear the %d tokens dynamic thinking may spend", cap, maxGeminiThinkingTokens)
	}
}

// Zero means thinking off, so the request has to carry the field. Sending no
// thinkingConfig would leave thinking on and uncapped, which is the opposite
// of what the operator asked for.
func TestZeroThinkingBudgetIsSentAsThinkingOff(t *testing.T) {
	t.Cleanup(ClearBuilderThinkingBudget)
	SetBuilderThinkingBudget(0)

	p := buildPayload(nil, wiredAgentGraph(), "add a step")
	gen := p["generationConfig"].(map[string]any)
	cfg, ok := gen["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatal("no thinkingConfig sent for a zero budget, so thinking stays on and uncapped")
	}
	if cfg["thinkingBudget"] != 0 {
		t.Errorf("thinkingBudget = %v, want 0", cfg["thinkingBudget"])
	}
}
