package nodes

import "testing"

func TestGeminiUsage(t *testing.T) {
	tests := []struct {
		name string
		resp map[string]any
		want TokenUsage
	}{
		{
			name: "full metadata",
			resp: map[string]any{"usageMetadata": map[string]any{
				"promptTokenCount":        float64(11842),
				"cachedContentTokenCount": float64(10240),
				"thoughtsTokenCount":      float64(612),
				"candidatesTokenCount":    float64(188),
			}},
			want: TokenUsage{Prompt: 11842, Cached: 10240, Thoughts: 612, Candidates: 188},
		},
		{
			// Gemini omits the field entirely when nothing was cached, which
			// must read as zero rather than breaking the whole line.
			name: "no cache field",
			resp: map[string]any{"usageMetadata": map[string]any{
				"promptTokenCount":     float64(900),
				"candidatesTokenCount": float64(40),
			}},
			want: TokenUsage{Prompt: 900, Candidates: 40},
		},
		{
			name: "no metadata at all",
			resp: map[string]any{"candidates": []any{}},
			want: TokenUsage{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := geminiUsage(tt.resp); got != tt.want {
				t.Fatalf("geminiUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTokenUsageAdd(t *testing.T) {
	a := TokenUsage{Prompt: 10, Cached: 5, Thoughts: 2, Candidates: 1}
	b := TokenUsage{Prompt: 3, Cached: 1, Thoughts: 4, Candidates: 2}
	want := TokenUsage{Prompt: 13, Cached: 6, Thoughts: 6, Candidates: 3}
	if got := a.Add(b); got != want {
		t.Fatalf("Add() = %+v, want %+v", got, want)
	}
}
