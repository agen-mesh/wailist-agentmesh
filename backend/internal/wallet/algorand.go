package wallet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/algorand/go-algorand-sdk/v2/client/v2/algod"
	"github.com/algorand/go-algorand-sdk/v2/crypto"
	"github.com/algorand/go-algorand-sdk/v2/mnemonic"
	"github.com/algorand/go-algorand-sdk/v2/transaction"
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

// WrapMnemonic derives the Algorand address from an existing raw mnemonic and
// returns it alongside an encrypted copy. Used for the platform wallet so the
// stored AgentWallet record is identical in shape to a generated one.
func (s *Service) WrapMnemonic(mn string) (address, encMnemonic string, err error) {
	privKey, err := mnemonic.ToPrivateKey(mn)
	if err != nil {
		return "", "", fmt.Errorf("invalid mnemonic: %w", err)
	}
	acc, err := crypto.AccountFromPrivateKey(privKey)
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
