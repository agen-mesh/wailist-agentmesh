package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/respond"
)

type arcBody struct {
	Op        string `json:"op"`
	Chain     string `json:"chain"`
	DestChain string `json:"destChain"`
	To        string `json:"to"`
	Amount    string `json:"amount"`
	Broadcast bool   `json:"broadcast"`
	TxHash    string `json:"txHash"`
}

type arcChainView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Family    string `json:"family"`
	ChainID   int64  `json:"chainId"`
	CAIP2     string `json:"caip2"`
	Domain    int    `json:"domain"`
	USDC      string `json:"usdc,omitempty"`
	EURC      string `json:"eurc,omitempty"`
	Explorer  string `json:"explorer,omitempty"`
	CCTP      bool   `json:"cctp"`
	Gateway   bool   `json:"gateway"`
	GasSymbol string `json:"gasSymbol,omitempty"`
	Testnet   bool   `json:"testnet"`
}

// ArcChains lists the chains for the server's network. A query network
// switches the list for a read. It never returns the spending key.
func (d *Deps) ArcChains(w http.ResponseWriter, r *http.Request) {
	cfg := d.arcConfig()
	if q := strings.TrimSpace(r.URL.Query().Get("network")); q != "" {
		cfg.Network = arc.ParseNetwork(q)
	}
	views := make([]arcChainView, 0, 8)
	for _, c := range arc.Chains(cfg.Network) {
		views = append(views, arcChainView{
			ID: c.ID, Name: c.Name, Family: c.Family, ChainID: c.ChainID,
			CAIP2: c.CAIP2(), Domain: c.Domain, USDC: c.USDC, EURC: c.EURC,
			Explorer: c.Explorer, CCTP: c.CCTP(), Gateway: c.Gateway(),
			GasSymbol: c.GasSymbol, Testnet: c.Testnet,
		})
	}
	address := ""
	if cfg.WalletConfigured() {
		address = cfg.Address.Hex()
	}
	respond.JSON(w, http.StatusOK, map[string]any{
		"network":          cfg.Network,
		"walletConfigured": cfg.WalletConfigured(),
		"address":          address,
		"payTo":            cfg.PayTo,
		"studioURL":        cfg.StudioURL,
		"maxUsdc":          arc.FormatUSDC(cfg.MaxMicros),
		"escrow":           cfg.Escrow,
		"keyError":         cfg.KeyError,
		"chains":           views,
	})
}

// ArcDiagram is the architecture the treasury page draws.
func (d *Deps) ArcDiagram(w http.ResponseWriter, r *http.Request) {
	cfg := d.arcConfig()
	body := arc.Diagram()
	body["studioURL"] = cfg.StudioURL
	body["network"] = cfg.Network
	respond.JSON(w, http.StatusOK, body)
}

// ArcTreasury reads ERC-20 USDC across the configured chains. Gateway and
// EURC come back as their own figures.
func (d *Deps) ArcTreasury(w http.ResponseWriter, r *http.Request) {
	cfg := d.arcConfig()
	if q := strings.TrimSpace(r.URL.Query().Get("network")); q != "" {
		cfg.Network = arc.ParseNetwork(q)
	}
	out, err := arc.ReadTreasury(r.Context(), arc.HTTPCaller{}, nil, cfg,
		r.URL.Query().Get("address"), r.URL.Query().Get("algorand"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, out)
}

// ArcQuote prepares calldata and an EIP-3009 description. Broadcast in the
// body is ignored: this route never submits a transaction.
func (d *Deps) ArcQuote(w http.ResponseWriter, r *http.Request) {
	body, err := readArcBody(r)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	body.Broadcast = false
	d.writeArcResult(w, r, body.Op, body, false)
}

func (d *Deps) ArcTransfer(w http.ResponseWriter, r *http.Request) {
	d.arcOp(w, r, "transfer")
}

func (d *Deps) ArcBridge(w http.ResponseWriter, r *http.Request) {
	d.arcOp(w, r, "bridge")
}

func (d *Deps) ArcGatewayDeposit(w http.ResponseWriter, r *http.Request) {
	d.arcOp(w, r, "gateway")
}

func (d *Deps) ArcNanopay(w http.ResponseWriter, r *http.Request) {
	d.arcOp(w, r, "nanopay")
}

func (d *Deps) arcOp(w http.ResponseWriter, r *http.Request, op string) {
	body, err := readArcBody(r)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	d.writeArcResult(w, r, op, body, body.Broadcast)
}

// ArcBridgeComplete asks Iris for the burn attestation and submits
// receiveMessage on the destination chain.
func (d *Deps) ArcBridgeComplete(w http.ResponseWriter, r *http.Request) {
	body, err := readArcBody(r)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := d.arcConfig()
	res, err := arc.CompleteBridge(r.Context(), arc.HTTPCaller{}, nil, cfg, body.Chain, body.DestChain, body.TxHash)
	if err != nil {
		code := arcErrorStatus(err)
		if res.Status == "pending" {
			code = http.StatusAccepted
			respond.JSON(w, code, res)
			return
		}
		respond.Error(w, code, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, res)
}

// ArcChallenge is the public 402 for an Arc nanopayment.
func (d *Deps) ArcChallenge(w http.ResponseWriter, r *http.Request) {
	cfg := d.arcConfig()
	var amount *big.Int
	if raw := strings.TrimSpace(r.URL.Query().Get("amount")); raw != "" {
		parsed, err := arc.ParseUSDCMicros(raw)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		amount = parsed
	}
	challenge, err := arc.NewChallenge(cfg, amount, r.URL.String())
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "not configured") {
			code = http.StatusServiceUnavailable
		}
		respond.Error(w, code, err.Error())
		return
	}
	respond.JSON(w, http.StatusPaymentRequired, challenge)
}

// ArcSettle accepts an x402 exact payment and submits the authorization.
// Operator authority is required because the relayer pays gas.
func (d *Deps) ArcSettle(w http.ResponseWriter, r *http.Request) {
	cfg := d.arcConfig()
	raw := r.Header.Get("X-Payment")
	if raw == "" {
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			respond.Error(w, http.StatusBadRequest, "unreadable payment")
			return
		}
		raw = string(b)
	}
	price, err := arc.ParseUSDCMicros("0.01")
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "price")
		return
	}
	if q := strings.TrimSpace(r.URL.Query().Get("amount")); q != "" {
		price, err = arc.ParseUSDCMicros(q)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	res, err := arc.SettlePayment(r.Context(), arc.HTTPCaller{}, cfg, raw, price)
	if err != nil {
		code := arcErrorStatus(err)
		if res.Status == "pending" && (res.TxHash != "" || res.ApproveTx != "") {
			respond.JSON(w, http.StatusAccepted, res)
			return
		}
		respond.Error(w, code, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, res)
}

// ArcBuy is the signed-in one-click nanopay. The platform key pays payTo.
func (d *Deps) ArcBuy(w http.ResponseWriter, r *http.Request) {
	body, err := readArcBody(r)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg := d.arcConfig()
	if body.To == "" {
		body.To = cfg.PayTo
	}
	if body.Amount == "" {
		body.Amount = "0.01"
	}
	d.writeArcResult(w, r, "nanopay", body, true)
}

func (d *Deps) writeArcResult(w http.ResponseWriter, r *http.Request, op string, body arcBody, broadcast bool) {
	cfg := d.arcConfig()
	res, err := arc.Execute(r.Context(), arc.HTTPCaller{}, cfg, arc.Request{
		Op:          op,
		ChainID:     body.Chain,
		DestChainID: body.DestChain,
		To:          body.To,
		Amount:      body.Amount,
		Broadcast:   broadcast,
	})
	if err != nil {
		code := arcErrorStatus(err)
		if res.Status == "pending" && (res.TxHash != "" || res.ApproveTx != "") {
			respond.JSON(w, http.StatusAccepted, res)
			return
		}
		respond.Error(w, code, err.Error())
		return
	}
	code := http.StatusOK
	if res.Status == "pending" {
		code = http.StatusAccepted
	}
	respond.JSON(w, code, res)
}

func arcErrorStatus(err error) int {
	if errors.Is(err, arc.ErrOperatorRequired) {
		return http.StatusForbidden
	}
	if strings.Contains(err.Error(), "not configured") {
		return http.StatusServiceUnavailable
	}
	return http.StatusBadRequest
}

func readArcBody(r *http.Request) (arcBody, error) {
	var body arcBody
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(&body); err != nil {
		return arcBody{}, errors.New("invalid request body")
	}
	return body, nil
}

func (d *Deps) arcConfig() arc.Config {
	if d.ArcConfig == nil {
		return arc.Config{}
	}
	return *d.ArcConfig
}
