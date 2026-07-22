package alert_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/alert"
)

func TestNotifyIsNoopWithoutWebhookURL(t *testing.T) {
	t.Setenv("ALERT_WEBHOOK_URL", "")
	// Must not panic or block — there's nowhere to send this.
	alert.Notify(context.Background(), "should not be sent")
}

func TestNotifyPostsMessageToWebhookURL(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		received <- body.Text
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("ALERT_WEBHOOK_URL", srv.URL)
	alert.Notify(context.Background(), "webhook signature rejected")

	select {
	case msg := <-received:
		if msg != "webhook signature rejected" {
			t.Fatalf("want %q got %q", "webhook signature rejected", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook POST")
	}
}
