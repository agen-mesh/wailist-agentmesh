package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

// bazaarResource is the shape returned by the Coinbase Bazaar discovery API.
type bazaarResource struct {
	ID          string         `json:"id"`
	URL         string         `json:"url"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Price       bazaarPrice    `json:"price"`
	InputSchema map[string]any `json:"inputSchema"`
	Tags        []string       `json:"tags"`
	Category    string         `json:"category"`
	Provider    string         `json:"provider"`
}

type bazaarPrice struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	Network  string `json:"network"`
}

// BazaarEndpoint is the normalized shape sent to the frontend.
type BazaarEndpoint struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	Provider         string            `json:"provider"`
	Price            string            `json:"price"`
	Unit             string            `json:"unit"`
	Category         string            `json:"category"`
	Tags             []string          `json:"tags"`
	Endpoint         string            `json:"endpoint"`
	DiscoveredParams []models.ParamDef `json:"discoveredParams,omitempty"`
	Source           string            `json:"source"` // always "bazaar"
}

func bazaarBase() string {
	if u := os.Getenv("BAZAAR_BASE_URL"); u != "" {
		return u
	}
	return "https://api.cdp.coinbase.com/platform/v2/x402/discovery"
}

// BazaarList proxies GET /marketplace/bazaar → Bazaar /resources.
func (d *Deps) BazaarList(w http.ResponseWriter, r *http.Request) {
	limit := r.URL.Query().Get("limit")
	offset := r.URL.Query().Get("offset")
	if limit == "" {
		limit = "24"
	}
	if offset == "" {
		offset = "0"
	}
	endpoints, err := fetchBazaar(r.Context(), fmt.Sprintf("/resources?limit=%s&offset=%s", limit, offset))
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bazaar unavailable")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

// BazaarSearch proxies GET /marketplace/bazaar/search?q= → Bazaar /search.
func (d *Deps) BazaarSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		respond.Error(w, http.StatusBadRequest, "q required")
		return
	}
	endpoints, err := fetchBazaar(r.Context(), "/search?query="+url.QueryEscape(q))
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "bazaar unavailable")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

func fetchBazaar(ctx context.Context, path string) ([]BazaarEndpoint, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bazaarBase()+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if key := os.Getenv("CDP_API_KEY"); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var raw struct {
		Resources []bazaarResource `json:"resources"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("bazaar decode: %w", err)
	}

	out := make([]BazaarEndpoint, 0, len(raw.Resources))
	for _, res := range raw.Resources {
		out = append(out, normalizeBazaarResource(res))
	}
	return out, nil
}

func normalizeBazaarResource(res bazaarResource) BazaarEndpoint {
	return BazaarEndpoint{
		ID:               "bazaar-" + res.ID,
		Name:             res.Name,
		Description:      res.Description,
		Provider:         res.Provider,
		Price:            res.Price.Amount,
		Unit:             "call",
		Category:         normalizeBazaarCategory(res.Category),
		Tags:             res.Tags,
		Endpoint:         res.URL,
		DiscoveredParams: extractParamDefs(res.InputSchema),
		Source:           "bazaar",
	}
}

func normalizeBazaarCategory(cat string) string {
	switch strings.ToLower(cat) {
	case "search":
		return "search"
	case "data", "analytics":
		return "data"
	case "ai", "ml", "nlp":
		return "ai"
	case "finance", "payments", "crypto":
		return "finance"
	case "media", "image", "video", "audio":
		return "media"
	default:
		return "util"
	}
}

func extractParamDefs(schema map[string]any) []models.ParamDef {
	if schema == nil {
		return nil
	}
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	requiredRaw, _ := schema["required"].([]any)
	requiredSet := make(map[string]bool, len(requiredRaw))
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	params := make([]models.ParamDef, 0, len(props))
	for name, v := range props {
		prop, _ := v.(map[string]any)
		typ, _ := prop["type"].(string)
		if typ == "" {
			typ = "string"
		}
		desc, _ := prop["description"].(string)
		params = append(params, models.ParamDef{
			Name:        name,
			Type:        typ,
			Required:    requiredSet[name],
			Description: desc,
		})
	}
	return params
}
