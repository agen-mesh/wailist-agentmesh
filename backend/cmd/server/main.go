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

	runner := engine.NewRunner(store, broker, walletSvc)
	runner.SetPlatformKeys(map[string]string{
		"gemini":    os.Getenv("PLATFORM_GEMINI_API_KEY"),
		"openai":    os.Getenv("PLATFORM_OPENAI_API_KEY"),
		"anthropic": os.Getenv("PLATFORM_ANTHROPIC_API_KEY"),
		"groq":      os.Getenv("PLATFORM_GROQ_API_KEY"),
		"mistral":   os.Getenv("PLATFORM_MISTRAL_API_KEY"),
	})

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

		Razorpay:      razorpayClient,
		RazorpayKeyID: razorpayClient.KeyID,
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

// expireStalePendingTransactionsLoop marks abandoned Razorpay checkouts (order created,
// never completed) as 'expired' so they stop being reported as in-progress. Runs on a
// fixed interval for the life of the process; errors are logged, not fatal.
func expireStalePendingTransactionsLoop(ctx context.Context, store *db.Store) {
	const (
		checkInterval = 5 * time.Minute
		staleAfter    = 30 * time.Minute
	)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for range ticker.C {
		n, err := store.ExpireStalePendingTransactions(ctx, staleAfter)
		if err != nil {
			log.Printf("expire stale pending transactions: %v", err)
			continue
		}
		if n > 0 {
			log.Printf("expired %d stale pending credit transactions", n)
		}
	}
}
