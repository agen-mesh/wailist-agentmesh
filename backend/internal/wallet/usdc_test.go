package wallet_test

import (
	"context"
	"encoding/base64"
	"os"
	"testing"

	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/v2/types"

	"github.com/agentmesh/backend/internal/wallet"
)

func testWalletService(t *testing.T) *wallet.Service {
	t.Helper()
	url := os.Getenv("TEST_ALGOD_URL")
	if url == "" {
		t.Skip("TEST_ALGOD_URL not set")
	}
	return wallet.NewService("test-enc-key-32-bytes-long-12345", url, "", "testnet")
}

func TestSignUSDCPaymentGroupProducesTwoTxnsWithCorrectIndex(t *testing.T) {
	svc := testWalletService(t)
	_, encMnemonic, err := svc.GenerateWallet()
	if err != nil {
		t.Fatal(err)
	}

	group, idx, err := svc.SignUSDCPaymentGroup(context.Background(), encMnemonic,
		"LXPC4GQPYH2EZQX2QDYMHCP2I7MXIZMVRPIYTQ3D7R7HXJ4SIHCSYLF5YA",
		10458941, 100000,
		"ZMFK2OI7ZBD2U27ISERZC4S6LKM6WMFJPZQ4MYNJDZ2VNBNMBA67RA22AA")
	if err != nil {
		t.Fatal(err)
	}
	if len(group) != 2 {
		t.Fatalf("want 2-txn group, got %d", len(group))
	}
	if idx != 0 {
		t.Fatalf("want paymentIndex 0, got %d", idx)
	}

	// txn0 must decode as a signed asset-transfer with the right amount.
	raw, err := base64.StdEncoding.DecodeString(group[0])
	if err != nil {
		t.Fatal(err)
	}
	var stx types.SignedTxn
	if err := msgpack.Decode(raw, &stx); err != nil {
		t.Fatal(err)
	}
	if stx.Txn.Type != types.AssetTransferTx {
		t.Fatalf("want AssetTransferTx, got %s", stx.Txn.Type)
	}
	if stx.Txn.AssetAmount != 100000 {
		t.Fatalf("want amount 100000, got %d", stx.Txn.AssetAmount)
	}
	if stx.Txn.XferAsset != 10458941 {
		t.Fatalf("want asset 10458941, got %d", stx.Txn.XferAsset)
	}

	// txn1 (fee-payer stub) must decode as unsigned (empty signature).
	raw1, err := base64.StdEncoding.DecodeString(group[1])
	if err != nil {
		t.Fatal(err)
	}
	var unsignedTxn types.Transaction
	if err := msgpack.Decode(raw1, &unsignedTxn); err != nil {
		t.Fatal(err)
	}
	if unsignedTxn.Type != types.PaymentTx {
		t.Fatalf("want fee-payer stub as PaymentTx, got %s", unsignedTxn.Type)
	}
}
