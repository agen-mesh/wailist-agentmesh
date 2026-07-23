package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
)

func main() {
	_ = godotenv.Load()

	ctx := context.Background()

	store, err := db.New(ctx, mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()

	broker := sse.NewBroker()

	walletSvc := wallet.NewService(
		mustEnv("ENCRYPTION_KEY"),
		envOr("ALGOD_URL", "https://testnet-api.algonode.cloud"),
		os.Getenv("ALGOD_TOKEN"),
		envOr("ALGORAND_NETWORK", "testnet"),
	)

	razorpayClient := payments.NewRazorpayClient(mustEnv("RAZORPAY_KEY_ID"), mustEnv("RAZORPAY_KEY_SECRET"), mustEnv("RAZORPAY_WEBHOOK_SECRET"))

	nowPaymentsClient := payments.NewNOWPaymentsClient(mustEnv("NOWPAYMENTS_API_KEY"), mustEnv("NOWPAYMENTS_IPN_SECRET"))
	if envOr("NOWPAYMENTS_SANDBOX", "false") == "true" {
		nowPaymentsClient.UseSandbox()
	}

	runner := engine.NewRunner(store, broker, walletSvc)

	go expireStalePendingTransactionsLoop(ctx, store)

	deps := &handlers.Deps{
		Store:         store,
		Broker:        broker,
		Wallet:        walletSvc,
		Engine:        runner,
		BaseURL:       envOr("BASE_URL", "http://localhost:8080"),
		JWTSecret:     mustEnv("JWT_SECRET"),
		EncryptionKey: mustEnv("ENCRYPTION_KEY"),

		FrontendURL:        envOr("FRONTEND_URL", "http://localhost:3000"),
		GithubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GithubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),

		SlackOAuthClientID:          os.Getenv("SLACK_OAUTH_CLIENT_ID"),
		SlackOAuthClientSecret:      os.Getenv("SLACK_OAUTH_CLIENT_SECRET"),
		GitHubConnectorClientID:     os.Getenv("GITHUB_CONNECTOR_CLIENT_ID"),
		GitHubConnectorClientSecret: os.Getenv("GITHUB_CONNECTOR_CLIENT_SECRET"),
		NotionClientID:              os.Getenv("NOTION_CLIENT_ID"),
		NotionClientSecret:          os.Getenv("NOTION_CLIENT_SECRET"),
		AirtableClientID:            os.Getenv("AIRTABLE_CLIENT_ID"),
		AirtableClientSecret:        os.Getenv("AIRTABLE_CLIENT_SECRET"),
		HubSpotClientID:             os.Getenv("HUBSPOT_CLIENT_ID"),
		HubSpotClientSecret:         os.Getenv("HUBSPOT_CLIENT_SECRET"),
		AsanaClientID:               os.Getenv("ASANA_CLIENT_ID"),
		AsanaClientSecret:           os.Getenv("ASANA_CLIENT_SECRET"),
		ClickUpClientID:             os.Getenv("CLICKUP_CLIENT_ID"),
		ClickUpClientSecret:         os.Getenv("CLICKUP_CLIENT_SECRET"),
		JiraClientID:                os.Getenv("JIRA_CLIENT_ID"),
		JiraClientSecret:            os.Getenv("JIRA_CLIENT_SECRET"),
		LinearClientID:              os.Getenv("LINEAR_CLIENT_ID"),
		LinearClientSecret:          os.Getenv("LINEAR_CLIENT_SECRET"),
		MailchimpClientID:           os.Getenv("MAILCHIMP_CLIENT_ID"),
		MailchimpClientSecret:       os.Getenv("MAILCHIMP_CLIENT_SECRET"),
		GitLabClientID:              os.Getenv("GITLAB_CLIENT_ID"),
		GitLabClientSecret:          os.Getenv("GITLAB_CLIENT_SECRET"),
		TrelloClientID:              os.Getenv("TRELLO_CLIENT_ID"),
		TrelloClientSecret:          os.Getenv("TRELLO_CLIENT_SECRET"),
		TodoistClientID:             os.Getenv("TODOIST_CLIENT_ID"),
		TodoistClientSecret:         os.Getenv("TODOIST_CLIENT_SECRET"),

		Razorpay:      razorpayClient,
		RazorpayKeyID: razorpayClient.KeyID,
		NOWPayments:   nowPaymentsClient,
	}

	r := api.NewRouter(deps)

	port := envOr("PORT", "8080")
	log.Printf("AgentMesh backend listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s not set", key)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// expireStalePendingTransactionsLoop marks abandoned checkouts (order/invoice created,
// never completed) as 'expired' so they stop being reported as in-progress. Runs on a
// fixed interval for the life of the process; errors are logged, not fatal. Sweeps each
// payment provider with its own staleness window: Razorpay checkouts are fast, so 30
// minutes of no completion means abandoned; NOWPayments crypto invoices settle on-chain
// and routinely take longer than that across multiple block confirmations, so they get a
// generous 24-hour window instead, to avoid marking real in-flight payments as expired
// mid-payment.
func expireStalePendingTransactionsLoop(ctx context.Context, store *db.Store) {
	const (
		checkInterval         = 5 * time.Minute
		razorpayStaleAfter    = 30 * time.Minute
		nowPaymentsStaleAfter = 24 * time.Hour
	)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := store.ExpireStalePendingTransactions(ctx, "razorpay", razorpayStaleAfter); err != nil {
			log.Printf("expire stale razorpay transactions: %v", err)
		} else if n > 0 {
			log.Printf("expired %d stale razorpay transactions", n)
		}
		if n, err := store.ExpireStalePendingTransactions(ctx, "nowpayments", nowPaymentsStaleAfter); err != nil {
			log.Printf("expire stale nowpayments transactions: %v", err)
		} else if n > 0 {
			log.Printf("expired %d stale nowpayments transactions", n)
		}
	}
}
