// Command x402pay is a one-shot x402 *client*: it pays a real, payment-gated
// endpoint once and prints what happened. Everything else in this repo plays
// the resource-server role (issue a 402, verify, settle); this plays the
// payer, which is otherwise only reachable by driving a whole workflow run.
//
// It exists mainly for one job that has no other clean trigger: the
// facilitator only catalogs a route in the Bazaar once it observes a valid
// discovery declaration riding on a *settled* payment, never on the
// informational challenge alone. So after a fix to that declaration ships,
// something has to make one genuine paid call before the catalog state
// changes. Hand-rolling that means reproducing the challenge decode, the
// fee-pooled group signing, and the header encoding -- all of which already
// exist here, in the exact form the relay itself uses.
//
// THIS SPENDS REAL MONEY. On mainnet the asset is real USDC and the transfer
// is irreversible. Guards, in order:
//
//   - It refuses to pay without -confirm. Without it, the command probes the
//     endpoint, prints the challenge and the exact amount it would send, and
//     exits 0 without signing anything. Run it that way first, every time.
//   - It refuses any quote above -max-micros (default $0.05).
//   - It refuses any asset other than -asset-id, so a target quoting some
//     other ASA can never be paid by accident.
//
// Secrets are read from the environment, not flags: a flag value is visible
// in `ps` output and lands in shell history. Set ENCRYPTION_KEY and
// X402PAY_ENC_MNEMONIC (or PLATFORM_SPEND_WALLET_ENC_MNEMONIC, which the
// deployed backend already defines) before running.
//
// Typical use -- dry run, then the real thing:
//
//	go run ./cmd/x402pay
//	go run ./cmd/x402pay -confirm
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/engine/nodes"
	"github.com/agentmesh/backend/internal/wallet"
)

// Mainnet/testnet constants, kept identical to cmd/server/main.go's own
// defaults. relayNetwork is the CAIP-style genesis-hash identifier the x402
// payload carries, which is a different string from the human network name
// the wallet service takes -- both are derived from -network here so they
// cannot disagree.
func main() {
	log.SetFlags(0)

	endpoint := flag.String("endpoint", "https://www.agent-mesh.app/api/x402/relay",
		"x402 endpoint to pay. Defaults to the relay's own self-listing")
	method := flag.String("method", "GET", "HTTP method for the paid request")
	bodyFlag := flag.String("body", "", "raw JSON body for a non-GET request. Sent on both the 402 probe and the paid retry, so a body-priced quote matches what is paid")
	maxMicros := flag.Int64("max-micros", 50000, "refuse to pay a quote above this many USDT base units (50000 = 0.05 USDT)")
	rpcURL := flag.String("rpc", chain.DefaultRPCURL, "Whitechain RPC, used only for balance reads")
	timeout := flag.Duration("timeout", 90*time.Second, "overall deadline for the probe and the paid call")
	confirm := flag.Bool("confirm", false, "actually sign and send the payment. Without this the command only probes and reports what it would pay")
	flag.Parse()

	if *maxMicros <= 0 {
		log.Fatalf("-max-micros must be positive, got %d", *maxMicros)
	}

	// The payer key comes from one of:
	//   X402PAY_PRIVATE_KEY            raw 0x hex key (local testing only)
	//   X402PAY_ENC_KEY                ENCRYPTION_KEY-encrypted key
	//   PLATFORM_SPEND_WALLET_ENC_KEY  Wallet 1
	encryptionKey := os.Getenv("ENCRYPTION_KEY")
	payerEnc := os.Getenv("X402PAY_ENC_KEY")
	if payerEnc == "" {
		payerEnc = os.Getenv("PLATFORM_SPEND_WALLET_ENC_KEY")
	}
	if raw := os.Getenv("X402PAY_PRIVATE_KEY"); raw != "" {
		// Encrypt the raw key under an ephemeral key so the normal signing
		// path (which only takes encrypted keys) can be used unchanged.
		encryptionKey = "x402pay-ephemeral-key-32-bytes!!"
		var err error
		if _, payerEnc, err = wallet.NewService(encryptionKey, *rpcURL).EncryptPrivateKey(raw); err != nil {
			log.Fatalf("X402PAY_PRIVATE_KEY: %v", err)
		}
	}
	if *confirm && (encryptionKey == "" || payerEnc == "") {
		log.Fatal("set X402PAY_PRIVATE_KEY, or ENCRYPTION_KEY plus X402PAY_ENC_KEY / PLATFORM_SPEND_WALLET_ENC_KEY")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	walletSvc := wallet.NewService(encryptionKey, *rpcURL)
	payerAddr := "(not resolved -- dry run needs no credentials)"
	if encryptionKey != "" && payerEnc != "" {
		addr, err := walletSvc.AddressForEncKey(payerEnc)
		if err != nil {
			log.Fatalf("could not derive the payer address: %v", err)
		}
		payerAddr = addr
		if bal, err := walletSvc.TokenBalance(ctx, addr); err == nil {
			payerAddr += fmt.Sprintf("  (%s %s)", chain.FormatUSDT(int64(bal)), chain.TokenSymbol)
		}
	}

	fmt.Printf("payer     : %s\n", payerAddr)
	fmt.Printf("endpoint  : %s %s\n", *method, *endpoint)
	fmt.Printf("network   : %s (%s %s)\n\n", chain.Network, chain.TokenSymbol, chain.USDTAddress)

	var probeBody []byte
	if *bodyFlag != "" && *method != "GET" {
		probeBody = []byte(*bodyFlag)
	}
	isV2, quote, err := nodes.ProbeX402QuoteWithBody(ctx, *endpoint, *method, probeBody, "application/json")
	if err != nil {
		log.Fatalf("could not read a 402 challenge from the endpoint: %v", err)
	}
	if !isV2 {
		log.Fatal("endpoint did not answer with an x402 v2 challenge -- refusing to pay a dialect this command cannot verify")
	}

	fmt.Println("challenge:")
	fmt.Printf("  network    : %v\n", quote.RawAccept["network"])
	fmt.Printf("  payTo      : %s\n", quote.PayTo)
	fmt.Printf("  asset      : %s\n", quote.Asset)
	fmt.Printf("  amount     : %s base units\n", quote.MaxAmountRequired)
	fmt.Printf("  extra      : %v\n", quote.RawAccept["extra"])
	fmt.Printf("  bazaar     : %s\n", bazaarSummary(quote.RawChallenge))

	if !*confirm {
		fmt.Printf("\nDRY RUN -- nothing signed, nothing spent.\n")
		fmt.Printf("Re-run with -confirm to send %s base units to %s.\n", quote.MaxAmountRequired, quote.PayTo)
		return
	}

	cfg := nodes.Wallet2PayConfig{
		Signer:                    walletSvc,
		PlatformWalletEncKey:      payerEnc,
		USDCAsset:                 chain.USDTAddress,
		RelayNetwork:              chain.Network,
		MaxRelayOutboundUSDMicros: *maxMicros,
	}

	fmt.Printf("\npaying...\n")
	result, err := nodes.PayTargetFromWallet2(ctx, cfg, *endpoint, *method, probeBody, quote)
	if err != nil {
		var payErr *nodes.Wallet2PayError
		if errors.As(err, &payErr) {
			err = fmt.Errorf("%s (status %d)", payErr.Msg, payErr.StatusCode)
		}
		if result.Signed {
			log.Fatalf("PAYMENT WAS SIGNED but the call did not complete cleanly: %v\n"+
				"Do NOT simply re-run this -- check %s first, the transfer may already have landed.", err, chain.AddressURL(payerAddr))
		}
		log.Fatalf("payment failed before anything was signed (no money moved): %v", err)
	}

	fmt.Printf("signed    : %t\n", result.Signed)
	fmt.Printf("settled   : %t\n", result.Settled)
	fmt.Printf("status    : %d\n", result.StatusCode)
	if result.OutboundTxID != "" {
		fmt.Printf("tx        : %s\n", chain.TxURL(result.OutboundTxID))
	}
	if len(result.ResponseBody) > 0 {
		fmt.Printf("response  : %s\n", truncate(string(result.ResponseBody), 600))
	}
	if !result.Settled {
		os.Exit(1)
	}
}

func bazaarSummary(challenge map[string]any) string {
	exts, _ := challenge["extensions"].(map[string]any)
	bazaar, _ := exts["bazaar"].(map[string]any)
	if bazaar == nil {
		return "absent -- this settlement will not catalog"
	}
	info, _ := bazaar["info"].(map[string]any)
	input, _ := info["input"].(map[string]any)
	declared, _ := input["method"].(string)

	schema, _ := bazaar["schema"].(map[string]any)
	props, _ := schema["properties"].(map[string]any)
	inSchema, _ := props["input"].(map[string]any)
	inProps, _ := inSchema["properties"].(map[string]any)
	methodSchema, _ := inProps["method"].(map[string]any)

	raw, _ := json.Marshal(methodSchema["enum"])
	var enum []any
	_ = json.Unmarshal(raw, &enum)

	if len(enum) == 1 && enum[0] == declared {
		return fmt.Sprintf("present, method %q matches enum %s", declared, raw)
	}
	return fmt.Sprintf("PRESENT BUT INVALID -- declared %q against enum %s; the catalog validator will drop this", declared, raw)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "... (truncated)"
}
