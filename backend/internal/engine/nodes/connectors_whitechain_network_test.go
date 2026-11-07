package nodes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentmesh/backend/internal/chain"
	"github.com/agentmesh/backend/internal/models"
)

func TestWhitechainConnectorsReportWhitechainNetworkWhileArcSettles(t *testing.T) {
	t.Cleanup(func() {
		chain.Use("")
		SetURLValidatorForTest(nil)
	})
	prevAPI := whitechainExplorerAPI
	t.Cleanup(func() { whitechainExplorerAPI = prevAPI })

	chain.Use("arc")
	if chain.Network == chain.Whitechain.Network {
		t.Fatal("arc profile did not change the active network")
	}

	addr := "0x1111111111111111111111111111111111111111"
	token := chain.Whitechain.TokenAddress
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/token-balances"):
			_, _ = w.Write([]byte("[]"))
		case strings.Contains(r.URL.Path, "/token-transfers"):
			_, _ = w.Write([]byte(`{"items":[]}`))
		case strings.Contains(r.URL.Path, "/tokens/"):
			_, _ = w.Write([]byte(`{"address_hash":"` + token + `","name":"Tether USD","symbol":"USDT","decimals":"6","total_supply":"1","type":"ERC-20","holders_count":"1"}`))
		default:
			_, _ = w.Write([]byte(`{"hash":"` + addr + `","coin_balance":"1000","is_contract":false}`))
		}
	}))
	defer srv.Close()
	SetURLValidatorForTest(func(string) error { return nil })
	SetWhitechainExplorer(srv.URL)

	account, err := fetchWhitechainAccount(context.Background(), models.WorkflowNode{
		Config: map[string]string{"wcAddress": addr},
	}, emptyRunContext{})
	if err != nil {
		t.Fatalf("account: %v", err)
	}
	if got := account.(map[string]any)["network"]; got != chain.Whitechain.Network {
		t.Fatalf("account network = %v, want %s", got, chain.Whitechain.Network)
	}

	txs, err := fetchWhitechainTransactions(context.Background(), models.WorkflowNode{
		Config: map[string]string{"wcAddress": addr},
	}, emptyRunContext{})
	if err != nil {
		t.Fatalf("transactions: %v", err)
	}
	if got := txs.(map[string]any)["network"]; got != chain.Whitechain.Network {
		t.Fatalf("transactions network = %v, want %s", got, chain.Whitechain.Network)
	}

	meta, err := fetchWhitechainToken(context.Background(), models.WorkflowNode{
		Config: map[string]string{"wcToken": token},
	}, emptyRunContext{})
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if got := meta.(map[string]any)["network"]; got != chain.Whitechain.Network {
		t.Fatalf("token network = %v, want %s", got, chain.Whitechain.Network)
	}
}
