package wallet_test

import (
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/wallet"
)

const encKey = "0123456789abcdef0123456789abcdef"

// freshMnemonic generates a real mnemonic using GenerateWallet so we don't
// hard-code a test mnemonic that might be flagged by secret scanners.
func freshMnemonic(t *testing.T) string {
	t.Helper()
	svc := wallet.NewService(encKey, "", "", "testnet")
	_, enc, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}
	mn, err := svc.DecryptMnemonic(enc)
	if err != nil {
		t.Fatal(err)
	}
	return mn
}

// TestWrapMnemonicRoundtrip verifies that WrapMnemonic:
//   - derives the correct Algorand address from the mnemonic
//   - encrypts the mnemonic with the service key (decryptable)
//   - returns the same address as GenerateWallet would for the same key
func TestWrapMnemonicRoundtrip(t *testing.T) {
	svc := wallet.NewService(encKey, "", "", "testnet")

	// Generate a fresh wallet the normal way to get a known mnemonic + address.
	origAddress, origEnc, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}
	origMnemonic, err := svc.DecryptMnemonic(origEnc)
	if err != nil {
		t.Fatal(err)
	}

	// Now wrap the same mnemonic — should recover the same address.
	wrapAddress, wrapEnc, err := svc.WrapMnemonic(origMnemonic)
	if err != nil {
		t.Fatalf("WrapMnemonic failed: %v", err)
	}
	if wrapAddress != origAddress {
		t.Fatalf("address mismatch: want %q got %q", origAddress, wrapAddress)
	}

	// The returned mnemonic should be decryptable and identical to the input.
	decrypted, err := svc.DecryptMnemonic(wrapEnc)
	if err != nil {
		t.Fatalf("cannot decrypt wrapped mnemonic: %v", err)
	}
	if decrypted != origMnemonic {
		t.Fatalf("decrypted mnemonic does not match original")
	}
}

// TestWrapMnemonicAddressLength verifies the returned address has the expected
// Algorand address length (58 chars).
func TestWrapMnemonicAddressLength(t *testing.T) {
	svc := wallet.NewService(encKey, "", "", "testnet")
	mn := freshMnemonic(t)

	address, _, err := svc.WrapMnemonic(mn)
	if err != nil {
		t.Fatal(err)
	}
	if len(address) != 58 {
		t.Fatalf("expected 58-char Algorand address, got %d chars: %q", len(address), address)
	}
}

// TestWrapMnemonicDecryptedIs25Words verifies the encrypted payload contains a
// valid 25-word Algorand mnemonic after decryption.
func TestWrapMnemonicDecryptedIs25Words(t *testing.T) {
	svc := wallet.NewService(encKey, "", "", "testnet")
	mn := freshMnemonic(t)

	_, enc, err := svc.WrapMnemonic(mn)
	if err != nil {
		t.Fatal(err)
	}
	decrypted, err := svc.DecryptMnemonic(enc)
	if err != nil {
		t.Fatal(err)
	}
	words := strings.Fields(decrypted)
	if len(words) != 25 {
		t.Fatalf("expected 25-word mnemonic, got %d words", len(words))
	}
}

// TestWrapMnemonicRejectsInvalid verifies that an invalid mnemonic returns an error.
func TestWrapMnemonicRejectsInvalid(t *testing.T) {
	svc := wallet.NewService(encKey, "", "", "testnet")
	_, _, err := svc.WrapMnemonic("not a valid mnemonic at all")
	if err == nil {
		t.Fatal("expected error for invalid mnemonic, got nil")
	}
}

// TestWrapMnemonicDifferentEncKey verifies that wrapping with a different service
// (different enc key) produces the same address (address is key-agnostic) but
// different ciphertext.
func TestWrapMnemonicDifferentEncKey(t *testing.T) {
	svc1 := wallet.NewService(encKey, "", "", "testnet")
	svc2 := wallet.NewService("fedcba9876543210fedcba9876543210", "", "", "testnet")
	mn := freshMnemonic(t)

	addr1, enc1, err := svc1.WrapMnemonic(mn)
	if err != nil {
		t.Fatal(err)
	}
	addr2, enc2, err := svc2.WrapMnemonic(mn)
	if err != nil {
		t.Fatal(err)
	}
	if addr1 != addr2 {
		t.Fatalf("addresses differ despite same mnemonic: %q vs %q", addr1, addr2)
	}
	if enc1 == enc2 {
		t.Fatal("ciphertexts should differ for different encryption keys")
	}
}
