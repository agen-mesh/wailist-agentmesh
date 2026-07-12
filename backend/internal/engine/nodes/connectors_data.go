package nodes

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/models"
)

// hubspotAPIBase is overridden in tests via SetHubSpotAPIBaseForTest.
var hubspotAPIBase = "https://api.hubapi.com"

// SetHubSpotAPIBaseForTest overrides the HubSpot API base URL. Call only
// from tests. Pass "" to reset to the real API.
func SetHubSpotAPIBaseForTest(base string) {
	if base == "" {
		hubspotAPIBase = "https://api.hubapi.com"
	} else {
		hubspotAPIBase = base
	}
}

func sendHubSpot(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "hubspotAPIKey")
	if apiKey == "" {
		return "hubspot_skipped_no_api_key", nil
	}
	payload := map[string]any{
		"properties": map[string]any{
			"hs_note_body": rc.Message(),
			"hs_timestamp": time.Now().UnixMilli(),
		},
	}
	headers := map[string]string{"Authorization": "Bearer " + apiKey}
	return postJSON(ctx, hubspotAPIBase+"/crm/v3/objects/notes", headers, payload, "hubspot_note_created", "HubSpot")
}

// mailchimpAPIBase is overridden in tests via SetMailchimpAPIBaseForTest —
// normally "https://{dc}.api.mailchimp.com" is built per-node from the API
// key's datacenter suffix, so the test override replaces the whole
// scheme+host and sendMailchimp skips that construction when set.
var mailchimpAPIBase = ""

// SetMailchimpAPIBaseForTest overrides the Mailchimp API base URL entirely.
// Call only from tests. Pass "" to reset to the real per-datacenter host.
func SetMailchimpAPIBaseForTest(base string) {
	mailchimpAPIBase = base
}

func sendMailchimp(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "mailchimpAPIKey")
	if apiKey == "" {
		return "mailchimp_skipped_no_api_key", nil
	}
	listID := configVal(node, "mailchimpListID", "")
	if listID == "" {
		return "mailchimp_skipped_no_list_id", nil
	}
	email := configVal(node, "mailchimpEmail", strings.TrimSpace(rc.Message()))
	if email == "" {
		return "mailchimp_skipped_no_email", nil
	}
	base := mailchimpAPIBase
	if base == "" {
		dc, err := mailchimpDatacenter(apiKey)
		if err != nil {
			return nil, err
		}
		base = "https://" + dc + ".api.mailchimp.com"
	}
	hash := md5.Sum([]byte(strings.ToLower(email)))
	subscriberHash := hex.EncodeToString(hash[:])
	target := base + "/3.0/lists/" + listID + "/members/" + subscriberHash
	payload := map[string]any{
		"email_address": email,
		"status_if_new": "subscribed",
		"status":        "subscribed",
	}
	auth := base64.StdEncoding.EncodeToString([]byte("anystring:" + apiKey))
	headers := map[string]string{"Authorization": "Basic " + auth}
	req, err := newJSONRequest(ctx, http.MethodPut, target, headers, payload)
	if err != nil {
		return nil, fmt.Errorf("Mailchimp: %w", err)
	}
	return doAndCheck(req, "mailchimp_subscriber_added", "Mailchimp")
}

// mailchimpDatacenter extracts the data-center suffix (e.g. "us21") from a
// Mailchimp API key of the form "<key>-<dc>".
func mailchimpDatacenter(apiKey string) (string, error) {
	i := strings.LastIndexByte(apiKey, '-')
	if i < 0 || i == len(apiKey)-1 {
		return "", fmt.Errorf("Mailchimp: API key missing datacenter suffix")
	}
	return apiKey[i+1:], nil
}

// MailchimpDatacenterForTest is a test-only exported wrapper for mailchimpDatacenter.
func MailchimpDatacenterForTest(apiKey string) (string, error) {
	return mailchimpDatacenter(apiKey)
}

func sendSupabase(ctx context.Context, node models.WorkflowNode, rc RunContexter) (any, error) {
	apiKey := secretVal(node, "supabaseAPIKey")
	if apiKey == "" {
		return "supabase_skipped_no_api_key", nil
	}
	projectURL := configVal(node, "supabaseProjectURL", "")
	table := configVal(node, "supabaseTable", "")
	if projectURL == "" || table == "" {
		return "supabase_skipped_missing_config", nil
	}
	column := configVal(node, "supabaseColumn", "content")
	target := strings.TrimRight(projectURL, "/") + "/rest/v1/" + url.PathEscape(table)
	if err := urlValidator(target); err != nil {
		return nil, err
	}
	payload := map[string]any{column: rc.Message()}
	headers := map[string]string{
		"apikey":        apiKey,
		"Authorization": "Bearer " + apiKey,
		"Prefer":        "return=minimal",
	}
	return postJSON(ctx, target, headers, payload, "supabase_row_inserted", "Supabase")
}
