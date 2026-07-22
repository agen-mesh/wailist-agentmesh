// backend/internal/engine/nodes/tier_test.go
package nodes_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/models"
)

func TestModelTier(t *testing.T) {
	cases := []struct {
		template string
		model    string
		want     string
	}{
		{"gemini", "gemini-2.5-flash", "economy"},
		{"gemini", "gemini-2.0-flash", "economy"},
		{"gemini", "gemini-1.5-flash", "economy"},
		{"gemini", "gemini-2.5-pro", "standard"},
		{"gemini", "gemini-1.5-pro", "standard"},
		{"openai", "gpt-4o-mini", "economy"},
		{"openai", "o4-mini", "economy"},
		{"openai", "gpt-4.1", "standard"},
		{"openai", "gpt-4o", "standard"},
		{"openai", "o3", "frontier"},
		{"anthropic", "claude-haiku-4-5", "economy"},
		{"anthropic", "claude-sonnet-4-6", "standard"},
		{"anthropic", "claude-3-5-sonnet-20241022", "standard"},
		{"anthropic", "claude-opus-4-8", "frontier"},
		{"groq", "llama-3.1-8b-instant", "economy"},
		{"groq", "gemma2-9b-it", "economy"},
		{"groq", "llama-3.3-70b-versatile", "standard"},
		{"groq", "mixtral-8x7b-32768", "standard"},
		{"mistral", "mistral-small-latest", "economy"},
		{"mistral", "codestral-latest", "economy"},
		{"mistral", "mistral-large-latest", "standard"},
		{"mistral", "mistral-medium-latest", "standard"},
		{"openai", "some-future-model-not-in-the-table", "standard"},
		{"unknown-template", "whatever", "standard"},
	}
	for _, c := range cases {
		t.Run(c.template+"/"+c.model, func(t *testing.T) {
			got := nodes.ModelTier(c.template, c.model)
			if got != c.want {
				t.Fatalf("ModelTier(%q, %q) = %q, want %q", c.template, c.model, got, c.want)
			}
		})
	}
}

func TestPlatformKeyFeeUSDMicros(t *testing.T) {
	cases := []struct {
		tier string
		want int64
	}{
		{"economy", models.PlatformKeyEconomyFeeUSDMicros},
		{"standard", models.PlatformKeyStandardFeeUSDMicros},
		{"frontier", models.PlatformKeyFrontierFeeUSDMicros},
		{"unrecognized-tier", models.PlatformKeyStandardFeeUSDMicros},
	}
	for _, c := range cases {
		t.Run(c.tier, func(t *testing.T) {
			got := nodes.PlatformKeyFeeUSDMicros(c.tier)
			if got != c.want {
				t.Fatalf("PlatformKeyFeeUSDMicros(%q) = %d, want %d", c.tier, got, c.want)
			}
		})
	}
}
