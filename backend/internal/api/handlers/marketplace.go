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
	Source           string            `json:"source"`       // always "bazaar"
	ChainFamily      string            `json:"chainFamily"`  // "evm", "avm", "svm"
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
	id := fmt.Sprintf("bazaar-%x", md5.Sum([]byte(item.Resource)))
	name, provider := parseNameProvider(item.ServiceName, item.Resource)
	return BazaarEndpoint{
		ID:               id,
		Name:             name,
		Description:      item.Description,
		Provider:         provider,
		Price:            formatBazaarPrice(item.Accepts),
		Unit:             "call",
		Category:         categoryFromTags(item.Tags, item.Type),
		Tags:             item.Tags,
		Endpoint:         item.Resource,
		DiscoveredParams: extractBazaarParams(item.Extensions),
		Source:           "bazaar",
		ChainFamily:      "evm",
	}
}

// parseNameProvider returns a human-readable name and provider for a Bazaar item.
// When serviceName is a URL (some Bazaar entries use the endpoint URL as the name),
// we derive the name from the URL path and the provider from the hostname.
func parseNameProvider(serviceName, resource string) (name, provider string) {
	isURL := strings.HasPrefix(serviceName, "http://") || strings.HasPrefix(serviceName, "https://")

	u, _ := url.Parse(resource)
	providerHost := ""
	if u != nil {
		providerHost = hostToProvider(u.Host)
	}

	if isURL {
		// serviceName is a URL — extract clean name from path
		su, err := url.Parse(serviceName)
		if err == nil {
			name = pathToName(su.Path)
		}
		if name == "" {
			name = providerHost
		}
		provider = providerHost
		return
	}

	// serviceName is a human-readable name — use it for both name and provider.
	name = serviceName
	provider = serviceName
	if provider == "" {
		provider = providerHost
	}
	// When serviceName is empty, derive a readable name from the resource URL path.
	if name == "" && u != nil {
		name = pathToName(u.Path)
	}
	if name == "" {
		name = providerHost
	}
	return
}

// hostToProvider converts a hostname like "x402.ottoai.services" → "OttoAI".
func hostToProvider(host string) string {
	// Strip port.
	if i := strings.LastIndex(host, ":"); i > strings.LastIndex(host, "]") {
		host = host[:i]
	}
	// Strip known subdomain prefixes.
	for _, pfx := range []string{"api.", "www.", "x402.", "skills.", "app."} {
		host = strings.TrimPrefix(host, pfx)
	}
	parts := strings.Split(host, ".")
	if len(parts) == 0 {
		return host
	}
	// Second-level domain is the brand name.
	brand := parts[0]
	if len(parts) >= 2 {
		brand = parts[len(parts)-2]
	}
	if brand == "" {
		return host
	}
	// Title-case each camelCase word boundary (e.g. "ottoai" → "OttoAI" is hard,
	// just capitalize the first letter for a clean result like "Ottoai").
	return strings.ToUpper(brand[:1]) + brand[1:]
}

// pathToName converts a URL path like "/api/chain/ens/:input" → "Ens".
func pathToName(path string) string {
	skip := map[string]bool{
		"api": true, "v1": true, "v2": true, "v3": true,
		"chain": true, "public": true, "data": true, "platform": true,
	}
	var segments []string
	for _, seg := range strings.Split(path, "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" || strings.HasPrefix(seg, ":") || strings.HasPrefix(seg, "{") {
			continue
		}
		if skip[strings.ToLower(seg)] {
			continue
		}
		segments = append(segments, seg)
	}
	if len(segments) == 0 {
		return ""
	}
	last := segments[len(segments)-1]
	words := strings.FieldsFunc(strings.NewReplacer("-", " ", "_", " ").Replace(last), func(r rune) bool { return r == ' ' })
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		}
	}
	return strings.Join(words, " ")
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
	case strings.Contains(combined, "search") || strings.Contains(combined, "query"):
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
