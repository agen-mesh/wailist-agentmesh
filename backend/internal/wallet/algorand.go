package wallet

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/algorand/go-algorand-sdk/v2/client/v2/algod"
	"github.com/algorand/go-algorand-sdk/v2/crypto"
	"github.com/algorand/go-algorand-sdk/v2/encoding/msgpack"
	"github.com/algorand/go-algorand-sdk/v2/mnemonic"
	"github.com/algorand/go-algorand-sdk/v2/transaction"
	"github.com/algorand/go-algorand-sdk/v2/types"
)

type Service struct {
	encKey     string
	algodURL   string
	algodToken string
	network    string
}

func NewService(encKey, algodURL, algodToken, network string) *Service {
	return &Service{encKey: encKey, algodURL: algodURL, algodToken: algodToken, network: network}
}

func (s *Service) Network() string { return s.network }

func (s *Service) GenerateWallet() (address, encMnemonic string, err error) {
	acc := crypto.GenerateAccount()
	mn, err := mnemonic.FromPrivateKey(acc.PrivateKey)
	if err != nil {
		return "", "", err
	}
	enc, err := Encrypt(mn, s.encKey)
	if err != nil {
		return "", "", err
	}
	return acc.Address.String(), enc, nil
}

func (s *Service) DecryptMnemonic(encMnemonic string) (string, error) {
	return Decrypt(encMnemonic, s.encKey)
}

func (s *Service) Balance(ctx context.Context, address string) (uint64, error) {
	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return 0, err
	}
	info, err := client.AccountInformation(address).Do(ctx)
	if err != nil {
		return 0, err
	}
	return info.Amount, nil
}

func (s *Service) FundFromDispenser(ctx context.Context, address string, amount uint64) (string, error) {
	url := fmt.Sprintf("https://dispenser.testnet.aws.algodev.network/?receiver=%s&amount=%d", address, amount)
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		TxID string `json:"txId"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.TxID, nil
}

func (s *Service) SignAndSendPayment(ctx context.Context, encMnemonic, toAddress string, microAlgo uint64) (string, error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return "", err
	}
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", err
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
	if err != nil {
		return "", err
	}

	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return "", err
	}
	params, err := client.SuggestedParams().Do(ctx)
	if err != nil {
		return "", err
	}
	txn, err := transaction.MakePaymentTxn(acc.Address.String(), toAddress, microAlgo, nil, "", params)
	if err != nil {
		return "", err
	}
	_, signed, err := crypto.SignTransaction(privKey, txn)
	if err != nil {
		return "", err
	}
	txID, err := client.SendRawTransaction(signed).Do(ctx)
	if err != nil {
		return "", err
	}
	return txID, nil
}

func (s *Service) OptInAsset(ctx context.Context, encMnemonic string, assetID uint64) (string, error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return "", err
	}
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", err
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
	if err != nil {
		return "", err
	}

	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return "", err
	}
	params, err := client.SuggestedParams().Do(ctx)
	if err != nil {
		return "", err
	}
	txn, err := transaction.MakeAssetAcceptanceTxn(acc.Address.String(), nil, params, assetID)
	if err != nil {
		return "", err
	}
	_, signed, err := crypto.SignTransaction(privKey, txn)
	if err != nil {
		return "", err
	}
	return client.SendRawTransaction(signed).Do(ctx)
}

// SignUSDCPaymentGroup builds a 2-txn atomic group for a gasless USDC payment:
// txn0 is the caller's signed asset-transfer (Fee=0, fee-pooled), txn1 is an
// unsigned payment-stub from feePayerAddr to itself that carries both txns'
// fees — the facilitator cosigns txn1 during /settle, so the caller's wallet
// never needs a standing ALGO balance for fees. Returns both txns base64
// (msgpack)-encoded in group order, and which index holds the real payment.
func (s *Service) SignUSDCPaymentGroup(ctx context.Context, encMnemonic, payTo string, assetID, amountMicros uint64, feePayerAddr string) (paymentGroup []string, paymentIndex int, err error) {
	mn, err := s.DecryptMnemonic(encMnemonic)
	if err != nil {
		return nil, 0, err
	}
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return nil, 0, err
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
	if err != nil {
		return nil, 0, err
	}

	client, err := algod.MakeClient(s.algodURL, s.algodToken)
	if err != nil {
		return nil, 0, err
	}
	params, err := client.SuggestedParams().Do(ctx)
	if err != nil {
		return nil, 0, err
	}

	payTxn, err := transaction.MakeAssetTransferTxn(acc.Address.String(), payTo, amountMicros, nil, params, "", assetID)
	if err != nil {
		return nil, 0, err
	}
	payTxn.Fee = 0 // fee-pooled: the stub below covers both txns' fees

	feeStub, err := transaction.MakePaymentTxn(feePayerAddr, feePayerAddr, 0, nil, "", params)
	if err != nil {
		return nil, 0, err
	}
	feeStub.Fee = types.MicroAlgos(params.MinFee * 2) // covers this txn + the fee-pooled payment txn

	grouped, err := transaction.AssignGroupID([]types.Transaction{payTxn, feeStub}, "")
	if err != nil {
		return nil, 0, err
	}

	_, signedPay, err := crypto.SignTransaction(privKey, grouped[0])
	if err != nil {
		return nil, 0, err
	}
	unsignedStubBytes := msgpack.Encode(grouped[1])

	return []string{
		base64.StdEncoding.EncodeToString(signedPay),
		base64.StdEncoding.EncodeToString(unsignedStubBytes),
	}, 0, nil
}
