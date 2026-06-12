package handlers

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/respond"
)

var goplausibleHTTPClient = &http.Client{Timeout: 15 * time.Second}

func goplausibleBase() string {
	if u := os.Getenv("GOPLAUSIBLE_BASE_URL"); u != "" {
		return u
	}
	return "https://facilitator.goplausible.xyz"
}

type gpAccept struct {
	Scheme  string `json:"scheme"`
	Network string `json:"network"`
	Amount  string `json:"amount"`
	PayTo   string `json:"payTo"`
}

type gpResource struct {
	ID            string         `json:"id"`
	ResourceURL   string         `json:"resourceUrl"`
	Method        string         `json:"method"`
	Description   string         `json:"description"`
	MimeType      string         `json:"mimeType"`
	MerchantID    string         `json:"merchantId"`
	Accepts       []gpAccept     `json:"accepts"`
	DiscoveryInfo map[string]any `json:"discoveryInfo"`
	VerifyCount   int            `json:"verifyCount"`
	SettleCount   int            `json:"settleCount"`
}

type gpMerchant struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Website     string   `json:"website"`
	Logo        string   `json:"logo"`
	Categories  []string `json:"categories"`
}

type gpResourcesResp struct {
	Items []gpResource `json:"items"`
}

type gpMerchantsResp struct {
	Items []gpMerchant `json:"items"`
}

func (d *Deps) GoplausibleList(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 100 {
		limit = 50
	}
	offset, err2 := strconv.Atoi(r.URL.Query().Get("offset"))
	if err2 != nil || offset < 0 {
		offset = 0
	}

	ctx := r.Context()
	resources, err := fetchGPResources(ctx, limit, offset)
	if err != nil {
		respond.Error(w, http.StatusBadGateway, "goplausible unavailable")
		return
	}

	merchants, _ := fetchGPMerchants(ctx)
	merchantMap := make(map[string]gpMerchant, len(merchants))
	for _, m := range merchants {
		merchantMap[m.ID] = m
	}

	endpoints := make([]BazaarEndpoint, 0, len(resources))
	for _, res := range resources {
		ep := normalizeGPResource(res, merchantMap)
		if ep.Endpoint != "" {
			endpoints = append(endpoints, ep)
		}
	}

	// When the live catalog is empty, fall back to GoPlausible's own example
	// endpoints so the Algorand section is never completely blank.
	if len(endpoints) == 0 {
		endpoints = gpExampleEndpoints()
	}

	respond.JSON(w, http.StatusOK, map[string]any{"endpoints": endpoints})
}

func fetchGPResources(ctx context.Context, limit, offset int) ([]gpResource, error) {
	url := fmt.Sprintf("%s/discovery/resources?limit=%d&offset=%d", goplausibleBase(), limit, offset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := goplausibleHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("goplausible %d: %s", resp.StatusCode, string(b))
	}

	var raw gpResourcesResp
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("goplausible decode: %w", err)
	}
	return raw.Items, nil
}

func fetchGPMerchants(ctx context.Context) ([]gpMerchant, error) {
	url := goplausibleBase() + "/discovery/merchants?limit=200"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := goplausibleHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, nil
	}

	var raw gpMerchantsResp
	json.NewDecoder(resp.Body).Decode(&raw)
	return raw.Items, nil
}

func normalizeGPResource(res gpResource, merchants map[string]gpMerchant) BazaarEndpoint {
	id := fmt.Sprintf("goplausible-%x", md5.Sum([]byte(res.ResourceURL)))

	name := res.Description
	provider := ""
	if m, ok := merchants[res.MerchantID]; ok {
		provider = m.Name
		if m.Name != "" {
			name = m.Name
		}
	}
	if name == "" {
		name = pathToName(urlPath(res.ResourceURL))
	}
	if name == "" {
		name = provider
	}

	return BazaarEndpoint{
		ID:               id,
		Name:             name,
		Description:      res.Description,
		Provider:         provider,
		Price:            gpFormatPrice(res.Accepts),
		Unit:             "call",
		Category:         gpCategory(res),
		Tags:             nil,
		Endpoint:         res.ResourceURL,
		DiscoveredParams: gpExtractParams(res.DiscoveryInfo),
		Source:           "goplausible",
		ChainFamily:      gpChainFamily(res.Accepts),
	}
}

func gpChainFamily(accepts []gpAccept) string {
	if len(accepts) == 0 {
		return "avm"
	}
	network := accepts[0].Network
	switch {
	case strings.HasPrefix(network, "algorand:") || strings.HasPrefix(network, "algorand-"):
		return "avm"
	case strings.HasPrefix(network, "eip155:"):
		return "evm"
	case strings.HasPrefix(network, "solana:"):
		return "svm"
	default:
		return "avm"
	}
}

func gpFormatPrice(accepts []gpAccept) string {
	if len(accepts) == 0 {
		return ""
	}
	amount, err := strconv.ParseFloat(accepts[0].Amount, 64)
	if err != nil || amount <= 0 {
		return ""
	}
	return fmt.Sprintf("%.4f", amount/1e6)
}

func gpCategory(res gpResource) string {
	combined := strings.ToLower(res.Description + " " + res.MimeType)
	switch {
	case strings.Contains(combined, "search") || strings.Contains(combined, "query"):
		return "search"
	case strings.Contains(combined, "finance") || strings.Contains(combined, "crypto") ||
		strings.Contains(combined, "price") || strings.Contains(combined, "market"):
		return "finance"
	case strings.Contains(combined, "image") || strings.Contains(combined, "video") ||
		strings.Contains(combined, "audio"):
		return "media"
	case strings.Contains(combined, " ai ") || strings.Contains(combined, "nlp") ||
		strings.Contains(combined, "sentiment"):
		return "ai"
	case strings.Contains(combined, "weather") || strings.Contains(combined, "data") ||
		strings.Contains(combined, "news"):
		return "data"
	default:
		return "util"
	}
}

func gpExtractParams(info map[string]any) []models.ParamDef {
	if info == nil {
		return nil
	}
	input, _ := info["input"].(map[string]any)
	if input == nil {
		return nil
	}
	qp, _ := input["queryParams"].(map[string]any)
	if len(qp) == 0 {
		return nil
	}
	params := make([]models.ParamDef, 0, len(qp))
	for name := range qp {
		params = append(params, models.ParamDef{Name: name, Type: "string", Required: false})
	}
	return params
}

func urlPath(rawURL string) string {
	s := rawURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		return s[i:]
	}
	return ""
}

// gpExampleEndpoints returns GoPlausible's own example AVM endpoints as seeds
// so the Algorand section shows real live resources while the catalog is new.
func gpExampleEndpoints() []BazaarEndpoint {
	return []BazaarEndpoint{
		{
			ID:          "goplausible-example-weather",
			Name:        "Weather Data",
			Description: "Real-time weather data by city. Returns temperature and conditions. Algorand testnet x402 demo endpoint by GoPlausible.",
			Provider:    "GoPlausible",
			Price:       "0.0010",
			Unit:        "call",
			Category:    "data",
			Tags:        []string{"weather", "demo", "algorand"},
			Endpoint:    "https://example.x402.goplausible.xyz/avm/weather",
			DiscoveredParams: []models.ParamDef{
				{Name: "city", Type: "string", Required: true, Description: "City name"},
			},
			Source:      "goplausible",
			ChainFamily: "avm",
		},
		{
			ID:          "goplausible-example-protected",
			Name:        "Protected Content",
			Description: "Pay-gated HTML content. Demonstrates Algorand x402 payment flow for web content. Algorand testnet demo by GoPlausible.",
			Provider:    "GoPlausible",
			Price:       "0.0010",
			Unit:        "call",
			Category:    "util",
			Tags:        []string{"protected", "demo", "algorand"},
			Endpoint:    "https://example.x402.goplausible.xyz/avm/protected",
			Source:      "goplausible",
			ChainFamily: "avm",
		},
	}
}
