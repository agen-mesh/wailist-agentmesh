// Package alert sends best-effort operational notifications for events that need a human
// to look, not just a log line — rejected webhook signatures, FX outages, and internal
// errors while completing or refunding a payment. Alerting is optional infrastructure: it
// must never block or fail the payment path it's reporting on.
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

var httpClient = &http.Client{Timeout: 5 * time.Second}

// Notify posts message to ALERT_WEBHOOK_URL as a Slack-compatible incoming webhook
// ({"text": message}). If the env var is unset, this is a no-op. Call as `go alert.Notify(...)`
// from request handlers so a slow or unreachable alerting endpoint never adds latency to the
// response Razorpay is waiting on.
func Notify(ctx context.Context, message string) {
	url := os.Getenv("ALERT_WEBHOOK_URL")
	if url == "" {
		return
	}

	body, err := json.Marshal(map[string]string{"text": message})
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
}
