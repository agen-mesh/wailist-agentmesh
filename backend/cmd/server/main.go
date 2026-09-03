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
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/payments"
	"github.com/agentmesh/backend/internal/scheduler"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/tendril"
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

	// Coupon catalog is configuration: COUPON_CODES="CODE:5,OTHER:12.50" (amounts
	// in USD). Unset means no redeemable codes at all, which is the safe default —
	// a code only grants credits while it's listed here. A malformed spec is fatal
	// rather than partially applied, so a typo can't silently disable one code in a
	// campaign while the rest stay live.
	couponSpec := os.Getenv("COUPON_CODES")
	catalog, err := db.ParseCouponCatalog(couponSpec)
	if err != nil {
		log.Fatalf("COUPON_CODES: %v", err)
	}
	store.SetCouponCatalog(catalog)
	if len(catalog) == 0 {
		log.Printf("no coupon codes configured (COUPON_CODES unset) — coupon redemption will reject every code")
	} else {
		log.Printf("coupon catalog loaded: %d code(s)", len(catalog))
	}

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

	usdcAssetID := uint64(10458941)                                         // testnet default
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

	// Stripe and PayPal are optional, unlike Cashfree and NOWPayments above:
	// leaving their credentials unset disables those checkout options rather
	// than refusing to boot. Their handlers fail closed with a clear message,
	// and the frontend reads /payments/providers to know which to offer.
	// Making them mustEnv would break every existing deployment on upgrade.
	//
	// Declared as the handler interface types, not the concrete pointers: a
	// nil *payments.StripeClient stored in a non-nil interface would make
	// d.Stripe != nil and defeat the nil checks the handlers rely on.
	var stripeClient handlers.StripeClient
	if key := os.Getenv("STRIPE_SECRET_KEY"); key != "" {
		// Stripe has no separate sandbox host -- a sk_test_ key IS test mode.
		stripeClient = payments.NewStripeClient(key, mustEnv("STRIPE_WEBHOOK_SECRET"))
	} else {
		log.Printf("STRIPE_SECRET_KEY not set — stripe checkout is disabled")
	}

	var payPalClient handlers.PayPalClient
	if id := os.Getenv("PAYPAL_CLIENT_ID"); id != "" {
		pp := payments.NewPayPalClient(id, mustEnv("PAYPAL_CLIENT_SECRET"), mustEnv("PAYPAL_WEBHOOK_ID"))
		if envOr("PAYPAL_SANDBOX", "false") == "true" {
			pp.UseSandbox()
		}
		payPalClient = pp
	} else {
		log.Printf("PAYPAL_CLIENT_ID not set — paypal checkout is disabled")
	}

	// $20.00 default, up from $5.00: Tendril's cheapest online machine is
	// $6.00/hour and a 2-hour rent tops the shared pool up by $12.00 in one
	// call, which the old ceiling rejected outright.
	maxRelayOutboundUSDMicros := envInt64Or("MAX_RELAY_OUTBOUND_USD_MICROS", 20_000_000)

	// Which /x402/relay a per-call paid tool402 request is routed through.
	// Defaults to BASE_URL, since in a real deployment the same instance
	// serves both — but they are genuinely different concerns, and conflating
	// them made a whole class of change untestable: BASE_URL also signs auth
	// cookies, so a developer who pointed it at their own machine broke login,
	// and one who left it alone had their local engine silently pay through
	// the DEPLOYED relay's code. Every outbound payment then came from a build
	// nobody was editing — including a payment-header bug that survived three
	// local restarts because the code that builds the header never ran here
	// (2026-08-03). Split so a local run can exercise its own relay.
	//
	// Note the relay call goes through the same SSRF-safe dialer as any other
	// outbound request, which refuses loopback and private ranges: a local
	// value must be a publicly-resolvable hostname for this machine (e.g. a
	// cloudflared/ngrok tunnel), not http://localhost:PORT.
	relayBaseURL := envOr("RELAY_BASE_URL", envOr("BASE_URL", "http://localhost:8080"))
	log.Printf("x402 relay base URL: %s", relayBaseURL)

	runner := engine.NewRunner(store, broker, walletSvc, relayBaseURL, platformSpendWalletEncMnemonic, mustEnv("ENCRYPTION_KEY"), engine.X402Config{
		PlatformWalletEncMnemonic: platformWalletEncMnemonic,
		USDCAssetID:               usdcAssetID,
		FacilitatorClient:         facilitatorClient,
		PlatformWalletAddress:     platformWalletAddr,
		RelayNetwork:              relayNetwork,
		RelayFeePayer:             relayFeePayer,
		MaxRelayOutboundUSDMicros: maxRelayOutboundUSDMicros,
		FrontendURL:               envOr("FRONTEND_URL", "http://localhost:3000"),
	})
	runner.SetPlatformKeys(map[string]string{
		"gemini":    os.Getenv("PLATFORM_GEMINI_API_KEY"),
		"openai":    os.Getenv("PLATFORM_OPENAI_API_KEY"),
		"anthropic": os.Getenv("PLATFORM_ANTHROPIC_API_KEY"),
		"groq":      os.Getenv("PLATFORM_GROQ_API_KEY"),
		"mistral":   os.Getenv("PLATFORM_MISTRAL_API_KEY"),
	})
	// Reuses the same Google app already configured for sign-in-with-Google
	// (below) -- Gmail/Sheets/Calendar/Drive nodes fail closed with a clear
	// error if unset, same pattern as SetTendril.
	runner.SetGoogleOAuth(os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"))

	var tendrilClient *tendril.Client
	var tendrilSession *tendril.Session
	if registryURL := envOr("TENDRIL_REGISTRY_URL", "https://tendrilregister.007575.xyz"); registryURL != "" {
		tendrilClient = tendril.NewClient(registryURL)
		// Wallet 2 is what pays Tendril through the relay, so Wallet 2's
		// address is the one Tendril keys the shared credit pool to — sign the
		// session with its mnemonic, not Wallet 1's.
		sess, err := tendrilClient.Session(ctx, walletSvc, platformWalletEncMnemonic)
		if err != nil {
			log.Printf("tendril: registry session unavailable (%v) — tendril nodes will fail closed", err)
		} else {
			tendrilSession = sess
			runner.SetTendril(tendrilClient, sess)
			log.Printf("tendril: registry %s, pool wallet %s", registryURL, platformWalletAddr)
		}
	}

	go expireStalePendingTransactionsLoop(ctx, store)
	runner.StartLeaseReaper(ctx, nodes.ReaperInterval)
	go scheduler.New(store, runner, broker, mustEnv("ENCRYPTION_KEY")).Run(ctx)

	deps := &handlers.Deps{
		Store:         store,
		Broker:        broker,
		Wallet:        walletSvc,
		Engine:        runner,
		BaseURL:       envOr("BASE_URL", "http://localhost:8080"),
		RelayBaseURL:  relayBaseURL,
		JWTSecret:     mustEnv("JWT_SECRET"),
		EncryptionKey: mustEnv("ENCRYPTION_KEY"),

		FrontendURL:        envOr("FRONTEND_URL", "http://localhost:3000"),
		GithubClientID:     os.Getenv("GITHUB_CLIENT_ID"),
		GithubClientSecret: os.Getenv("GITHUB_CLIENT_SECRET"),
		GoogleClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),

		// Empty falls back to GoPlausible's facilitator (see
		// defaultBazaarBaseURL) — set only to point at a mirror or a fake.
		BazaarBaseURL: os.Getenv("BAZAAR_BASE_URL"),

		Cashfree:      cashfreeClient,
		CashfreeAppID: cashfreeClient.AppID,
		NOWPayments:   nowPaymentsClient,
		Stripe:        stripeClient,
		PayPal:        payPalClient,

		PlatformWalletAddress:          platformWalletAddr,
		PlatformWalletEncMnemonic:      platformWalletEncMnemonic,
		PlatformSpendWalletEncMnemonic: platformSpendWalletEncMnemonic,
		PlatformGeminiAPIKey:           os.Getenv("PLATFORM_GEMINI_API_KEY"),
		FacilitatorClient:              facilitatorClient,
		USDCAssetID:                    usdcAssetID,
		RelayNetwork:                   relayNetwork,
		RelayFeePayer:                  relayFeePayer,
		USDCSigner:                     walletSvc,
		MaxRelayOutboundUSDMicros:      maxRelayOutboundUSDMicros,
		TendrilClient:                  tendrilClient,
		TendrilSession:                 tendrilSession,

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
// payment provider with its own staleness window, since what counts as "abandoned"
// differs by how the provider settles -- see the constants below.
func expireStalePendingTransactionsLoop(ctx context.Context, store *db.Store) {
	const (
		checkInterval = 5 * time.Minute
		// Hosted card checkouts are fast; 30 minutes without completion means
		// the payer walked away.
		cardStaleAfter = 30 * time.Minute
		// Stripe is NOT just a card checkout: its delayed-notification methods
		// (ACH, SEPA debit, Boleto, OXXO, Konbini) complete the session
		// immediately as 'unpaid' and only settle hours or days later, via
		// async_payment_succeeded. A 30-minute window stamped those rows
		// 'expired' while they were still legitimately in flight. No credit was
		// lost (CompleteCreditTransaction only refuses rows marked 'failed'),
		// but the row went expired -> completed and misreported itself in
		// purchase history for the whole gap.
		stripeStaleAfter = 7 * 24 * time.Hour
		// Crypto invoices settle on-chain and routinely take longer.
		nowPaymentsStaleAfter = 24 * time.Hour
		// PayPal orders can sit approved-but-uncaptured while a payer finishes
		// on another device, so they get more room than a card checkout.
		payPalStaleAfter = 3 * time.Hour
	)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for range ticker.C {
		for _, p := range []struct {
			provider string
			after    time.Duration
		}{
			{"cashfree", cardStaleAfter},
			{"stripe", stripeStaleAfter},
			{"paypal", payPalStaleAfter},
			{"nowpayments", nowPaymentsStaleAfter},
		} {
			if n, err := store.ExpireStalePendingTransactions(ctx, p.provider, p.after); err != nil {
				log.Printf("expire stale %s transactions: %v", p.provider, err)
			} else if n > 0 {
				log.Printf("expired %d stale %s transactions", n, p.provider)
			}
		}
	}
}
