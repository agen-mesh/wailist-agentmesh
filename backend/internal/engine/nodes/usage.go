package nodes

import "fmt"

// TokenUsage is one Gemini call's billed token counts.
//
// Cached tokens are billed at a tenth of the input rate, and thinking tokens
// at the OUTPUT rate, so a build's real cost cannot be read off Prompt alone
// -- which is why all four are kept apart rather than summed.
type TokenUsage struct {
	Prompt     int
	Cached     int
	Thoughts   int
	Candidates int
}

// Add returns the sum of two rounds' usage.
func (u TokenUsage) Add(o TokenUsage) TokenUsage {
	return TokenUsage{
		Prompt:     u.Prompt + o.Prompt,
		Cached:     u.Cached + o.Cached,
		Thoughts:   u.Thoughts + o.Thoughts,
		Candidates: u.Candidates + o.Candidates,
	}
}

// String renders one log line's worth.
func (u TokenUsage) String() string {
	return fmt.Sprintf("prompt=%d cached=%d thoughts=%d out=%d", u.Prompt, u.Cached, u.Thoughts, u.Candidates)
}

// geminiUsage reads usageMetadata off a generateContent response.
//
// Every field is optional: Gemini omits cachedContentTokenCount entirely when
// nothing was cached, and a response that failed to decode has no metadata at
// all. A missing field reads as zero rather than failing the build -- this is
// instrumentation, and instrumentation never breaks the thing it measures.
func geminiUsage(resp map[string]any) TokenUsage {
	meta, _ := resp["usageMetadata"].(map[string]any)
	if meta == nil {
		return TokenUsage{}
	}
	num := func(k string) int {
		v, _ := meta[k].(float64)
		return int(v)
	}
	return TokenUsage{
		Prompt:     num("promptTokenCount"),
		Cached:     num("cachedContentTokenCount"),
		Thoughts:   num("thoughtsTokenCount"),
		Candidates: num("candidatesTokenCount"),
	}
}
