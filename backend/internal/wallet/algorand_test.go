package wallet_test

import (
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/wallet"
)

func TestGenerateWallet(t *testing.T) {
	svc := wallet.NewService("0123456789abcdef0123456789abcdef",
		"https://testnet-api.algonode.cloud", "", "testnet")

	address, encMnemonic, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}
	if len(address) < 50 {
		t.Fatalf("address too short: %q", address)
	}
	if encMnemonic == "" {
		t.Fatal("no encrypted mnemonic")
	}

	mnem, err := svc.DecryptMnemonic(encMnemonic)
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Fields(mnem)
	if len(words) != 25 {
		t.Fatalf("want 25 words got %d", len(words))
	}
}
