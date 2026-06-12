package handlers

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

var bazaarHTTPClient = &http.Client{Timeout: 15 * time.Second}

// bazaarItem reflects the actual Coinbase Bazaar API item shape.
type bazaarItem struct {
	Resource    string        `json:"resource"`    // endpoint URL
	ServiceName string        `json:"serviceName"` // name + provider
	Description string        `json:"description"`
	Tags        []string      `json:"tags"`
	Type        string        `json:"type"`
	Accepts     []bazaarAccept `json:"accepts"`
	Extensions  bazaarExts    `json:"extensions"`
}

type bazaarAccept struct {
	Amount  string `json:"amount"` // micro-units; USDC has 6 decimals (1000 = $0.001)
	Network string `json:"network"`
}

type bazaarExts struct {
	Bazaar struct {
		Schema map[string]any `json:"schema"`
	} `json:"bazaar"`
}

// bazaarResp handles both list ("items") and search ("resources") response shapes.
type bazaarResp struct {
	Items     []bazaarItem `json:"items"`
	Resources []bazaarItem `json:"resources"`
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
	limit, err2 := strconv.Atoi(r.URL.Query().Get("limit"))
	if err2 != nil || limit <= 0 || limit > 100 {
		limit = 24
	}
	offset, err3 := strconv.Atoi(r.URL.Query().Get("offset"))
	if err3 != nil || offset < 0 {
		offset = 0
	}
	endpoints, err := fetchBazaar(r.Context(), fmt.Sprintf("/resources?limit=%d&offset=%d", limit, offset))
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

	resp, err := bazaarHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("bazaar %d: %s", resp.StatusCode, string(b))
	}

	var raw bazaarResp
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("bazaar decode: %w", err)
	}

	// List endpoint returns "items"; search endpoint returns "resources".
	items := raw.Items
	if len(items) == 0 {
		items = raw.Resources
	}

	out := make([]BazaarEndpoint, 0, len(items))
	for _, item := range items {
		if ep := normalizeBazaarItem(item); ep.Endpoint != "" {
			out = append(out, ep)
		}
	}
	return out, nil
}

func normalizeBazaarItem(item bazaarItem) BazaarEndpoint {
	// Stable ID derived from the endpoint URL.
	id := fmt.Sprintf("bazaar-%x", md5.Sum([]byte(item.Resource)))

	name := item.ServiceName
	if name == "" {
		name = item.Resource
	}

	return BazaarEndpoint{
		ID:               id,
		Name:             name,
		Description:      item.Description,
		Provider:         item.ServiceName,
		Price:            formatBazaarPrice(item.Accepts),
		Unit:             "call",
		Category:         categoryFromTags(item.Tags, item.Type),
		Tags:             item.Tags,
		Endpoint:         item.Resource,
		DiscoveredParams: extractBazaarParams(item.Extensions),
		Source:           "bazaar",
	}
}

// formatBazaarPrice converts micro-USDC to a human-readable dollar string.
// Bazaar amounts are in the token's smallest unit; USDC has 6 decimal places,
// so 1000 micro-USDC = $0.001.
func formatBazaarPrice(accepts []bazaarAccept) string {
	if len(accepts) == 0 {
		return ""
	}
	amount, err := strconv.ParseFloat(accepts[0].Amount, 64)
	if err != nil || amount <= 0 {
		return ""
	}
	usd := amount / 1e6
	if usd < 0.001 {
		return fmt.Sprintf("%.6f", usd)
	}
	if usd < 0.01 {
		return fmt.Sprintf("%.4f", usd)
	}
	return fmt.Sprintf("%.3f", usd)
}

// categoryFromTags derives our frontend category from Bazaar tags and type.
func categoryFromTags(tags []string, typ string) string {
	combined := strings.ToLower(strings.Join(tags, " ") + " " + typ)
	switch {
	case strings.ContainsAny(combined, "") && (strings.Contains(combined, "search") || strings.Contains(combined, "query")):
		return "search"
	case strings.Contains(combined, "finance") || strings.Contains(combined, "crypto") ||
		strings.Contains(combined, "stock") || strings.Contains(combined, "market") ||
		strings.Contains(combined, "payment") || strings.Contains(combined, "price"):
		return "finance"
	case strings.Contains(combined, "image") || strings.Contains(combined, "video") ||
		strings.Contains(combined, "media") || strings.Contains(combined, "audio"):
		return "media"
	case strings.Contains(combined, " ai ") || strings.Contains(combined, "nlp") ||
		strings.Contains(combined, " ml ") || strings.Contains(combined, "llm") ||
		strings.Contains(combined, "sentiment"):
		return "ai"
	case strings.Contains(combined, "weather") || strings.Contains(combined, "data") ||
		strings.Contains(combined, "analytics") || strings.Contains(combined, "news"):
		return "data"
	default:
		return "util"
	}
}

// extractBazaarParams reads query param definitions from the Bazaar schema.
// The schema nests them at: schema.properties.input.properties.queryParams.properties
func extractBazaarParams(exts bazaarExts) []models.ParamDef {
	schema := exts.Bazaar.Schema
	if schema == nil {
		return nil
	}
	// Navigate: schema → properties → input → properties → queryParams
	props, _ := schema["properties"].(map[string]any)
	input, _ := props["input"].(map[string]any)
	inputProps, _ := input["properties"].(map[string]any)
	queryParams, _ := inputProps["queryParams"].(map[string]any)
	qpProps, _ := queryParams["properties"].(map[string]any)
	if len(qpProps) == 0 {
		return nil
	}

	requiredRaw, _ := queryParams["required"].([]any)
	requiredSet := make(map[string]bool, len(requiredRaw))
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	params := make([]models.ParamDef, 0, len(qpProps))
	for name, v := range qpProps {
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

// extractParamDefs is kept for backward compatibility with tests.
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
