// Command walletgen provisions a Whitechain (EVM) key for one of AgentMesh's
// platform wallets and prints its address plus an ENCRYPTION_KEY-encrypted
// private key ready to paste into env vars. Run this locally, never in CI or
// committed anywhere: its stdout is a secret.
//
//	go run ./cmd/walletgen -enc-key $ENCRYPTION_KEY               # fresh key
//	go run ./cmd/walletgen -enc-key $ENCRYPTION_KEY -import 0x…   # existing key
//
// Platform wallets never need WBT for x402: they only sign EIP-3009
// authorizations, and the facilitator pays the gas. They need USDT.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/wallet"
)

func main() {
	encKey := flag.String("enc-key", "", "32-byte ENCRYPTION_KEY (required, must match the deployed backend's ENCRYPTION_KEY)")
	importKey := flag.String("import", "", "0x hex private key to encrypt instead of generating one; pass - to read it from stdin")
	rpcURL := flag.String("rpc", chain.DefaultRPCURL, "Whitechain RPC used to print the wallet's current balances")
	flag.Parse()

	if *encKey == "" {
		log.Fatal("-enc-key is required")
	}
	svc := wallet.NewService(*encKey, *rpcURL)

	var address, enc string
	var err error
	switch *importKey {
	case "":
		address, enc, err = svc.GenerateWallet()
	case "-":
		line, rerr := bufio.NewReader(os.Stdin).ReadString('\n')
		if rerr != nil && line == "" {
			log.Fatalf("read key from stdin: %v", rerr)
		}
		address, enc, err = svc.EncryptPrivateKey(strings.TrimSpace(line))
	default:
		address, enc, err = svc.EncryptPrivateKey(*importKey)
	}
	if err != nil {
		log.Fatalf("walletgen: %v", err)
	}

	fmt.Printf("ADDRESS=%s\n", address)
	fmt.Printf("ENC_KEY=%s\n", enc)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if usdt, err := svc.TokenBalance(ctx, address); err == nil {
		fmt.Fprintf(os.Stderr, "%s balance: %s %s\n", chain.Name, chain.FormatUSDT(int64(usdt)), chain.TokenSymbol)
	}
	fmt.Fprintf(os.Stderr, "Fund %s with testnet %s (%s). WBT is optional: the facilitator pays gas. Explorer: %s\n",
		address, chain.TokenSymbol, chain.USDTAddress, chain.AddressURL(address))
}
