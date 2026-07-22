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
	t.Setenv("DISCORD_WEBHOOK_PAYMENTS_URL", "")
	// Must not panic or block — there's nowhere to send this.
	alert.Notify(context.Background(), alert.ChannelPayments, "should not be sent")
}

func TestNotifyPostsDiscordFormattedMessage(t *testing.T) {
	received := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		received <- body.Content
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("DISCORD_WEBHOOK_PAYMENTS_URL", srv.URL)
	alert.Notify(context.Background(), alert.ChannelPayments, "webhook signature rejected")

	select {
	case msg := <-received:
		if msg != "webhook signature rejected" {
			t.Fatalf("want %q got %q", "webhook signature rejected", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook POST")
	}
}

func TestNotifyRoutesToCorrectChannel(t *testing.T) {
	paymentsReceived := make(chan bool, 1)
	creditsReceived := make(chan bool, 1)

	paymentsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paymentsReceived <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer paymentsSrv.Close()
	creditsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		creditsReceived <- true
		w.WriteHeader(http.StatusOK)
	}))
	defer creditsSrv.Close()

	t.Setenv("DISCORD_WEBHOOK_PAYMENTS_URL", paymentsSrv.URL)
	t.Setenv("DISCORD_WEBHOOK_CREDITS_URL", creditsSrv.URL)

	alert.Notify(context.Background(), alert.ChannelCredits, "credited $2.00")

	select {
	case <-creditsReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for credits webhook POST")
	}
	select {
	case <-paymentsReceived:
		t.Fatal("payments webhook should not have received this message")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestNotifyLogsNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	t.Setenv("DISCORD_WEBHOOK_WORKFLOWS_URL", srv.URL)
	// Must not panic — failure is logged, not propagated.
	alert.Notify(context.Background(), alert.ChannelWorkflows, "workflow run failed")
}
