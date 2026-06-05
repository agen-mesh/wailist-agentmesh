package wallet_test

import (
	"testing"

	"github.com/agentmesh/backend/internal/wallet"
)

func TestEncryptDecrypt(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef"
	plain := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"

	enc, err := wallet.Encrypt(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if enc == plain {
		t.Fatal("should be encrypted")
	}

	dec, err := wallet.Decrypt(enc, key)
	if err != nil {
		t.Fatal(err)
	}
	if dec != plain {
		t.Fatalf("want %q got %q", plain, dec)
	}
}
