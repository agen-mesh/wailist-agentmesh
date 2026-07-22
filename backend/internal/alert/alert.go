// Package alert posts live audit-log messages to Discord channels via incoming webhooks —
// one webhook URL per channel/category. Alerting is optional infrastructure: a missing or
// unreachable webhook must never block or fail the payment/workflow path it's reporting on.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

// Channel identifies which Discord channel (i.e. which webhook URL) a message goes to.
type Channel int

const (
	// ChannelPayments carries payment failure modes: rejected webhook signatures, FX
	// outages, internal errors completing or refunding a transaction.
	ChannelPayments Channel = iota
	// ChannelCredits carries every successful credit and refund — the money audit trail.
	ChannelCredits
	// ChannelWorkflows carries workflow run lifecycle events: start, finish, fail.
	ChannelWorkflows
)

var channelEnvVar = map[Channel]string{
	ChannelPayments:  "DISCORD_WEBHOOK_PAYMENTS_URL",
	ChannelCredits:   "DISCORD_WEBHOOK_CREDITS_URL",
	ChannelWorkflows: "DISCORD_WEBHOOK_WORKFLOWS_URL",
}

var httpClient = &http.Client{Timeout: 5 * time.Second}

// Notify posts message to the Discord webhook configured for channel. If that channel's
// env var is unset, this is a no-op. Call as `go alert.Notify(...)` from request handlers
// so a slow or unreachable webhook never adds latency to the caller.
func Notify(ctx context.Context, channel Channel, message string) {
	url := os.Getenv(channelEnvVar[channel])
	if url == "" {
		return
	}

	body, err := json.Marshal(map[string]string{"content": message})
	if err != nil {
		log.Printf("alert: marshal failed: %v", err)
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		log.Printf("alert: build request failed: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("alert: notify failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("alert: webhook returned status %d for channel %d", resp.StatusCode, channel)
	}
}
