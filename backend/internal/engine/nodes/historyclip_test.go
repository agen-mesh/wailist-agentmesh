package nodes

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClipHistoryText(t *testing.T) {
	long := strings.Repeat("x", 2000)
	tests := []struct {
		name     string
		role     string
		text     string
		maxRunes int
		wantHas  string
	}{
		{"a user turn is kept whole, however long", "user", long, len(long), "x"},
		{"a long model turn is clipped", "model", long, maxReplayedModelChars, "x"},
		{"a short model turn is untouched", "model", "added a coingecko step", len("added a coingecko step"), "coingecko"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clipHistoryText(tt.role, tt.text)
			if n := utf8.RuneCountInString(got); n > tt.maxRunes {
				t.Errorf("clipped to %d runes, want at most %d", n, tt.maxRunes)
			}
			if !strings.Contains(got, tt.wantHas) {
				t.Errorf("the turn lost its content: %q", got)
			}
		})
	}
}

// TestClipHistoryTextDropsTestRunTrailers: withTestStatus appends these to a
// reply. Replayed, they restate a test result for a graph that has since
// changed, which costs tokens and is how a stale value gets quoted back to
// the user a turn or two later.
func TestClipHistoryTextDropsTestRunTrailers(t *testing.T) {
	tests := []struct {
		name    string
		trailer string
	}{
		{"tested answer", "\n\n**Test run answer:** BTC is $64,102.11 as of today."},
		{"not tested since the change", "\n\n_This workflow has not been test-run since it was last changed, so it has not been checked yet._"},
		{"test produced nothing", "\n\n_The last test run did not produce an answer: step \"Price\" returned {}._"},
		{"unverified steps", "\n\n_Not checked by the test run: \"Notify\" (a send is only simulated)._"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clipHistoryText("model", "Built it."+tt.trailer)
			if got != "Built it." {
				t.Errorf("clipHistoryText() = %q, want just the reply %q", got, "Built it.")
			}
		})
	}
}

func TestClipHistoryTextKeepsValidUTF8(t *testing.T) {
	got := clipHistoryText("model", strings.Repeat("é", maxReplayedModelChars*2))
	if !utf8.ValidString(got) {
		t.Fatal("a clipped multi-byte turn is not valid UTF-8, which Gemini rejects")
	}
}

// TestBuildGraphReplaysClippedHistory checks the wiring, not just the helper:
// what actually reaches Gemini must be the clipped turn.
func TestBuildGraphReplaysClippedHistory(t *testing.T) {
	bodies := scriptedGemini(t, []string{text("done")})
	stale := "Built the price workflow.\n\n**Test run answer:** BTC is $64,102.11 as of today."
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey:  "k",
		Message: "rename the agent",
		Graph:   wiredAgentGraph(),
		History: []BuildTurn{
			{Role: "user", Text: "build me a bitcoin price workflow"},
			{Role: "model", Text: stale},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*bodies) == 0 {
		t.Fatal("no request reached the model")
	}
	first := (*bodies)[0]
	if !strings.Contains(first, "Built the price workflow.") {
		t.Error("the replayed model turn is missing from the request")
	}
	if strings.Contains(first, "64,102.11") {
		t.Error("a stale test-run value from an earlier turn was replayed to the model")
	}
}

// TestBuildGraphSkipsATurnThatClipsToNothing: a model turn that was only a
// test-run note clips to "", and Gemini rejects an empty part. It must be
// dropped from the replay, not sent empty.
func TestBuildGraphSkipsATurnThatClipsToNothing(t *testing.T) {
	bodies := scriptedGemini(t, []string{text("done")})
	_, err := BuildGraph(context.Background(), BuildRequest{
		APIKey:  "k",
		Message: "rename the agent",
		Graph:   wiredAgentGraph(),
		History: []BuildTurn{
			{Role: "user", Text: "is it tested?"},
			{Role: "model", Text: trailerUntested},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*bodies) == 0 {
		t.Fatal("no request reached the model")
	}
	if first := (*bodies)[0]; strings.Contains(first, `"text":""`) {
		t.Error("an empty part was replayed to the model")
	}
}
