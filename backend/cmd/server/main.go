package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/x402"
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

	platformWalletAddr := os.Getenv("PLATFORM_WALLET_ADDRESS")
	platformWalletEncMnemonic := os.Getenv("PLATFORM_WALLET_ENC_MNEMONIC")
	if platformWalletAddr == "" || platformWalletEncMnemonic == "" {
		log.Fatal("PLATFORM_WALLET_ADDRESS and PLATFORM_WALLET_ENC_MNEMONIC must both be set — the platform wallet's payTo address must stay fixed for the whole competition, so it is provisioned once out-of-band, never auto-generated at startup")
	}
	if derivedAddr, err := walletSvc.AddressForEncMnemonic(platformWalletEncMnemonic); err != nil {
		log.Fatalf("PLATFORM_WALLET_ENC_MNEMONIC does not decrypt/derive a valid Algorand address: %v", err)
	} else if derivedAddr != platformWalletAddr {
		log.Fatalf("PLATFORM_WALLET_ADDRESS (%s) does not match the address derived from PLATFORM_WALLET_ENC_MNEMONIC (%s) — this wallet's address is published as payTo in every x402 challenge while its mnemonic signs the outbound relay leg, so a mismatch means inbound payments accumulate in one account while a different account signs outbound payments", platformWalletAddr, derivedAddr)
	}

	platformSpendWalletAddr := os.Getenv("PLATFORM_SPEND_WALLET_ADDRESS")
	platformSpendWalletEncMnemonic := os.Getenv("PLATFORM_SPEND_WALLET_ENC_MNEMONIC")
	if platformSpendWalletAddr == "" || platformSpendWalletEncMnemonic == "" {
		log.Fatal("PLATFORM_SPEND_WALLET_ADDRESS and PLATFORM_SPEND_WALLET_ENC_MNEMONIC must both be set — Wallet 1 pays every relayed x402 call on behalf of users' credit balances, so it is provisioned once out-of-band via cmd/walletgen, never auto-generated at startup")
	}
	if derivedAddr, err := walletSvc.AddressForEncMnemonic(platformSpendWalletEncMnemonic); err != nil {
		log.Fatalf("PLATFORM_SPEND_WALLET_ENC_MNEMONIC does not decrypt/derive a valid Algorand address: %v", err)
	} else if derivedAddr != platformSpendWalletAddr {
		log.Fatalf("PLATFORM_SPEND_WALLET_ADDRESS (%s) does not match the address derived from PLATFORM_SPEND_WALLET_ENC_MNEMONIC (%s) — these must be the same wallet", platformSpendWalletAddr, derivedAddr)
	}

	usdcAssetID := uint64(10458941) // testnet default
	relayNetwork := "algorand:SGO1GKSzyE7IEPItTxCByw9x8FmnrCDexi9/cOUJOiI=" // testnet default
	relayFeePayer := "ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA"
	if envOr("ALGORAND_NETWORK", "testnet") == "mainnet" {
		usdcAssetID = 31566704
		relayNetwork = "algorand:wGHE2Pwdvd7S12BL5FaOP20EGYesN73ktiC1qzkkit8="
	}

	facilitatorClient := x402.NewFacilitatorClient(envOr("FACILITATOR_URL", "https://facilitator.goplausible.xyz"))

	cashfreeClient := payments.NewCashfreeClient(mustEnv("CASHFREE_APP_ID"), mustEnv("CASHFREE_SECRET_KEY"))
	if envOr("CASHFREE_SANDBOX", "false") == "true" {
		cashfreeClient.UseSandbox()
	}

	nowPaymentsClient := payments.NewNOWPaymentsClient(mustEnv("NOWPAYMENTS_API_KEY"), mustEnv("NOWPAYMENTS_IPN_SECRET"))
	if envOr("NOWPAYMENTS_SANDBOX", "false") == "true" {
		nowPaymentsClient.UseSandbox()
	}

	runner := engine.NewRunner(store, broker, walletSvc, envOr("BASE_URL", "http://localhost:8080"), platformSpendWalletEncMnemonic, usdcAssetID)

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

		Cashfree:      cashfreeClient,
		CashfreeAppID: cashfreeClient.AppID,
		NOWPayments:   nowPaymentsClient,

		PlatformWalletAddress:     platformWalletAddr,
		PlatformWalletEncMnemonic: platformWalletEncMnemonic,
		FacilitatorClient:         facilitatorClient,
		USDCAssetID:               usdcAssetID,
		RelayNetwork:              relayNetwork,
		RelayFeePayer:             relayFeePayer,
		USDCSigner:                walletSvc,
		MaxRelayOutboundUSDMicros: envInt64Or("MAX_RELAY_OUTBOUND_USD_MICROS", 5_000_000), // $5.00 default

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

// envInt64Or parses key as a base-10 int64, falling back to fallback if
// unset or unparseable (logging a warning in the latter case rather than
// silently ignoring a misconfigured value).
func envInt64Or(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		log.Printf("%s=%q is not a valid integer, using default %d", key, v, fallback)
		return fallback
	}
	return n
}

// expireStalePendingTransactionsLoop marks abandoned checkouts (order/invoice created,
// never completed) as 'expired' so they stop being reported as in-progress. Runs on a
// fixed interval for the life of the process; errors are logged, not fatal. Sweeps each
// payment provider with its own staleness window: Cashfree checkouts are fast, so 30
// minutes of no completion means abandoned; NOWPayments crypto invoices settle on-chain
// and routinely take longer, so they get a generous 24-hour window.
func expireStalePendingTransactionsLoop(ctx context.Context, store *db.Store) {
	const (
		checkInterval         = 5 * time.Minute
		razorpayStaleAfter    = 30 * time.Minute
		nowPaymentsStaleAfter = 24 * time.Hour
	)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := store.ExpireStalePendingTransactions(ctx, "cashfree", razorpayStaleAfter); err != nil {
			log.Printf("expire stale cashfree transactions: %v", err)
		} else if n > 0 {
			log.Printf("expired %d stale cashfree transactions", n)
		}
		if n, err := store.ExpireStalePendingTransactions(ctx, "nowpayments", nowPaymentsStaleAfter); err != nil {
			log.Printf("expire stale nowpayments transactions: %v", err)
		} else if n > 0 {
			log.Printf("expired %d stale nowpayments transactions", n)
		}
	}
}
