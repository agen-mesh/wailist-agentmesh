package nodes

import "github.com/agentmesh/backend/internal/models"

// modelTiers buckets every model exposed in the frontend's Provider node
// dropdowns (frontend/src/components/canvas/Inspector.tsx ProviderInspector)
// into a pricing tier by real published per-token cost, following Zapier's
// flat-multiplier billing pattern. Keep this in sync with that dropdown list
// and with frontend/src/lib/data.ts's MODEL_TIERS (display-only mirror) —
// there is no single source of truth shared across the Go/TS boundary.
var modelTiers = map[string]map[string]string{
	"gemini": {
		"gemini-2.5-flash": "economy",
		"gemini-2.0-flash": "economy",
		"gemini-1.5-flash": "economy",
		"gemini-2.5-pro":   "standard",
		"gemini-1.5-pro":   "standard",
	},
	"openai": {
		"gpt-4o-mini": "economy",
		"o4-mini":     "economy",
		"gpt-4.1":     "standard",
		"gpt-4o":      "standard",
		"o3":          "frontier",
	},
	"anthropic": {
		"claude-haiku-4-5":           "economy",
		"claude-sonnet-4-6":          "standard",
		"claude-3-5-sonnet-20241022": "standard",
		"claude-opus-4-8":            "frontier",
	},
	"groq": {
		"llama-3.1-8b-instant":    "economy",
		"gemma2-9b-it":            "economy",
		"llama-3.3-70b-versatile": "standard",
		"mixtral-8x7b-32768":      "standard",
	},
	"mistral": {
		"mistral-small-latest":  "economy",
		"codestral-latest":      "economy",
		"mistral-large-latest":  "standard",
		"mistral-medium-latest": "standard",
	},
}

// ModelTier classifies a (template, model) pair into a pricing tier. Unknown
// templates or models default to "standard" — never "economy", so an
// unrecognized frontier-class model added by a future config change can't
// silently undercharge.
func ModelTier(template, model string) string {
	if byModel, ok := modelTiers[template]; ok {
		if tier, ok := byModel[model]; ok {
			return tier
		}
	}
	return "standard"
}

// PlatformKeyFeeUSDMicros maps a tier to its flat per-call credit charge.
func PlatformKeyFeeUSDMicros(tier string) int64 {
	switch tier {
	case "economy":
		return models.PlatformKeyEconomyFeeUSDMicros
	case "frontier":
		return models.PlatformKeyFrontierFeeUSDMicros
	default:
		return models.PlatformKeyStandardFeeUSDMicros
	}
}
