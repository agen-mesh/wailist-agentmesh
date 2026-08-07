// Command walletgen provisions an Algorand account for use as one of
// AgentMesh's platform wallets and prints its address plus an
// ENCRYPTION_KEY-encrypted mnemonic ready to paste into env vars. Run this
// locally, never in CI or committed anywhere — its stdout is a secret.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	sdkcrypto "github.com/algorand/go-algorand-sdk/v2/crypto"
	"github.com/algorand/go-algorand-sdk/v2/mnemonic"

	"github.com/agentmesh/backend/internal/wallet"
)

func main() {
	encKey := flag.String("enc-key", "", "32-byte hex ENCRYPTION_KEY (required, must match the deployed backend's ENCRYPTION_KEY)")
	algodURL := flag.String("algod-url", "https://mainnet-api.algonode.cloud", "algod REST endpoint used for the opt-in transaction")
	algodToken := flag.String("algod-token", "", "algod API token, if the endpoint requires one")
	network := flag.String("network", "mainnet", "algorand network: mainnet or testnet")
	assetID := flag.Uint64("asset-id", 0, "USDC ASA id to opt into (mainnet 31566704, testnet 10458941); defaults to mainnet if not specified")
	importMnemonic := flag.String("import-mnemonic", "", "25-word Algorand mnemonic to import instead of generating a fresh account (e.g. one already generated in Pera or Defly)")
	skipOptIn := flag.Bool("skip-opt-in", false, "skip the USDC opt-in transaction (do this if the account isn't funded with ALGO yet; opt in separately once it is)")
	optInOnly := flag.String("opt-in-only", "", "an already-encrypted mnemonic printed by a prior walletgen run — opt it into the USDC asset now that the account is funded, skipping generation/import entirely")
	showMnemonic := flag.Bool("show-mnemonic", false, "also print the raw (unencrypted) 25-word mnemonic to stderr, once — for writing down or importing into another wallet app (e.g. Pera's Algo25/legacy import). Off by default; the encrypted mnemonic on stdout is the only output needed for normal env-var provisioning")
	flag.Parse()

	if *encKey == "" {
		log.Fatal("-enc-key is required")
	}

	// Derive asset-id from network if not explicitly set
	if *assetID == 0 {
		if *network == "mainnet" {
			*assetID = 31566704
		} else {
			*assetID = 10458941
		}
	}

	svc := wallet.NewService(*encKey, *algodURL, *algodToken, *network)

	// Handle opt-in-only mode
	if *optInOnly != "" {
		txID, err := svc.OptInAsset(context.Background(), *optInOnly, *assetID)
		if err != nil {
			log.Fatalf("USDC opt-in failed: %v", err)
		}
		fmt.Fprintf(os.Stderr, "Opted into asset %d (txid %s)\n", *assetID, txID)
		return
	}

	var address, encMnemonic string
	var err error
	if *importMnemonic != "" {
		address, encMnemonic, err = importWallet(*importMnemonic, *encKey)
	} else {
		address, encMnemonic, err = svc.GenerateWallet()
	}
	if err != nil {
		log.Fatalf("wallet setup failed: %v", err)
	}

	fmt.Fprintf(os.Stderr, "Address: %s\n", address)
	fmt.Fprintf(os.Stderr, "Fund this address with ALGO (fees + min balance) and USDC (spend balance) before use.\n")

	if *showMnemonic {
		mn, derr := svc.DecryptMnemonic(encMnemonic)
		if derr != nil {
			log.Fatalf("failed to decrypt mnemonic for display: %v", derr)
		}
		fmt.Fprintln(os.Stderr, "\n=== RAW MNEMONIC — write this down now, or import it directly into a wallet app (shown once) ===")
		fmt.Fprintln(os.Stderr, mn)
		fmt.Fprintln(os.Stderr, "=== do not paste the above anywhere except a wallet app's own import/recovery screen ===")
	}

	if !*skipOptIn {
		txID, err := svc.OptInAsset(context.Background(), encMnemonic, *assetID)
		if err != nil {
			// Print the encrypted mnemonic on its own line rather than
			// interpolated into the error text, so it's a clean value to
			// copy for the -opt-in-only retry instead of being embedded in a
			// quoted, wrapped log line.
			fmt.Println(encMnemonic)
			log.Fatalf("USDC opt-in failed: %v (fund the address with a small amount of ALGO first, then re-run with -opt-in-only using the encrypted mnemonic printed above)", err)
		}
		fmt.Fprintf(os.Stderr, "Opted into asset %d (txid %s)\n", *assetID, txID)
	}

	fmt.Println(encMnemonic)
}

func importWallet(mn, encKey string) (address, encMnemonic string, err error) {
	sk, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", "", fmt.Errorf("invalid mnemonic: %w", err)
	}
	acc, err := sdkcrypto.AccountFromPrivateKey(sk)
	if err != nil {
		return "", "", err
	}
	enc, err := wallet.Encrypt(mn, encKey)
	if err != nil {
		return "", "", err
	}
	return acc.Address.String(), enc, nil
}
