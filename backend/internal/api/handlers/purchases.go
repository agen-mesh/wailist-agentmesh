package handlers

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/arkiv"
	"github.com/agentmesh/backend/internal/config"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/respond"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/world"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/go-chi/chi/v5"
)

type PurchaseRuntime struct {
	Service          *purchase.Service
	Catalog          purchase.Catalog
	Identity         *purchase.WorldIdentity
	Merchant         http.Handler
	ValidateEndpoint func(context.Context, string) error
	PublicConfig     map[string]any
}

func (d *Deps) SetupPurchases(ctx context.Context, c config.Purchase) error {
	public := map[string]any{"enabled": false, "blockedReasons": c.BlockedReasons, "chainId": purchase.ChainID, "asset": purchase.Asset, "rpcUrl": "https://rpc.testnet.arc.network", "merchantCapability": merchant.Capability, "arkivAvailable": false, "worldAvailable": false, "evidenceMode": "live"}
	policy := purchase.NewEndpointPolicy()
	d.Purchases = &PurchaseRuntime{PublicConfig: public, ValidateEndpoint: policy.Validate}
	if c.ArkivRPCURL != "" {
		catalog, err := arkiv.NewClient(c.ArkivRPCURL, policy.Validate)
		if err == nil {
			d.Purchases.Catalog = purchase.ArkivCatalog{Client: catalog}
			public["arkivAvailable"] = true
		}
	}
	if !c.Enabled || len(c.BlockedReasons) > 0 {
		return nil
	}
	if len(c.EncryptionKey) != 32 || d.Store == nil || d.Purchases.Catalog == nil {
		return purchase.ErrDisabled
	}
	key, err := crypto.HexToECDSA(strings.TrimPrefix(c.RelayerKey, "0x"))
	if err != nil {
		return purchase.ErrDisabled
	}
	gas, ok := new(big.Int).SetString(c.MaxGasWei, 10)
	if !ok || gas.Sign() <= 0 || gas.BitLen() > 256 {
		return purchase.ErrDisabled
	}
	publicKey, err := hex.DecodeString(strings.TrimPrefix(c.MerchantPublicKey, "0x"))
	if err != nil || len(publicKey) != ed25519.PublicKeySize || len(c.MerchantBearer) < 32 {
		return purchase.ErrDisabled
	}
	payee, err := purchase.Address(c.MerchantPayee)
	if err != nil {
		return purchase.ErrDisabled
	}
	price, err := purchase.Amount(c.MerchantPriceMicros)
	if err != nil || price.Sign() <= 0 {
		return purchase.ErrDisabled
	}
	if err = policy.Validate(ctx, c.MerchantResource); err != nil {
		return purchase.ErrDisabled
	}
	rail := &purchase.ArcRail{Caller: arc.HTTPCaller{}, RPC: c.RPCURL, Key: key, MaxGasWei: gas, Confirmations: c.Confirmations}
	u, err := url.Parse(c.MerchantResource)
	if err != nil {
		return purchase.ErrDisabled
	}
	client := &purchase.MerchantClient{Client: policy.Client(), Policy: policy.Validate, BaseURL: u.Scheme + "://" + u.Host + "/merchant", Resource: c.MerchantResource, Bearer: c.MerchantBearer, PublicKey: publicKey, Payee: payee, PriceMicros: c.MerchantPriceMicros}
	issuer := world.ProductionIssuer
	if c.WorldEnvironment == string(world.Sandbox) {
		issuer = world.SandboxIssuer
	}
	identityStore := d.Store.PurchaseIdentities(c.EncryptionKey)
	identityClient, err := world.New(ctx, world.Config{Enabled: true, Environment: world.Environment(c.WorldEnvironment), Issuer: issuer, ClientID: c.WorldClientID, RedirectURI: c.CallbackURL, AuthMethod: world.ClientAuthMethod(c.WorldAuthMethod), ClientSecret: c.WorldClientSecret, PrivateKeyPEM: c.WorldPrivateKeyPEM, PrivateKeyID: c.WorldPrivateKeyID, RequiredACR: c.WorldACR}, identityStore, nil)
	if err != nil {
		return purchase.ErrDisabled
	}
	identity := &purchase.WorldIdentity{Client: identityClient, Store: identityStore, Issuer: issuer}
	s := &purchase.Service{Enabled: true, Store: d.Store.Purchases(), Catalog: d.Purchases.Catalog, Merchant: client, Identity: identity, Rail: rail, Domain: c.FrontendURL, Seal: func(s string) (string, error) { return wallet.Encrypt(s, c.EncryptionKey) }, Open: func(s string) (string, error) { return wallet.Decrypt(s, c.EncryptionKey) }}
	var merchantHandler http.Handler
	if c.MerchantSigningKey != "" {
		private, err := hex.DecodeString(strings.TrimPrefix(c.MerchantSigningKey, "0x"))
		if err != nil || len(private) != ed25519.PrivateKeySize || !ed25519.PrivateKey(private).Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(publicKey)) {
			return purchase.ErrDisabled
		}
		service, err := merchant.New(merchant.Config{Resource: c.MerchantResource, Payee: payee, AmountMicros: c.MerchantPriceMicros, Capability: merchant.Capability, PrivateKey: private}, d.Store.MerchantPurchases(), rail.MerchantVerifier)
		if err != nil {
			return purchase.ErrDisabled
		}
		merchantHandler, err = merchant.NewHandler(service, c.MerchantBearer)
		if err != nil {
			return purchase.ErrDisabled
		}
	}
	public["enabled"] = true
	public["blockedReasons"] = []string{}
	public["arkivAvailable"] = true
	public["worldAvailable"] = true
	d.Purchases.Service = s
	d.Purchases.Identity = identity
	d.Purchases.Merchant = merchantHandler
	if d.Engine != nil {
		d.Engine.SetPurchases(s)
	}
	return nil
}

func (d *Deps) PurchaseConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if d.Purchases == nil {
		respond.JSON(w, http.StatusOK, map[string]any{"enabled": false, "blockedReasons": []string{"Purchases are not configured"}, "chainId": purchase.ChainID, "asset": purchase.Asset, "rpcUrl": "", "merchantCapability": "text.digest.v1", "arkivAvailable": false, "worldAvailable": false, "evidenceMode": "live"})
		return
	}
	respond.JSON(w, http.StatusOK, d.Purchases.PublicConfig)
}
func (d *Deps) purchaseService(w http.ResponseWriter, r *http.Request) (*purchase.Service, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	account, _ := r.Context().Value(CtxUserID).(string)
	if account == "" {
		respond.Error(w, http.StatusUnauthorized, "Sign in to manage purchases")
		return nil, "", false
	}
	if d.Purchases == nil || d.Purchases.Service == nil || !d.Purchases.Service.Enabled {
		respond.Error(w, http.StatusServiceUnavailable, "Purchases are not configured")
		return nil, "", false
	}
	return d.Purchases.Service, account, true
}
func purchaseBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 128<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if decoder.Decode(dest) != nil {
		respond.Error(w, http.StatusBadRequest, "Invalid purchase request")
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		respond.Error(w, http.StatusBadRequest, "Invalid purchase request")
		return false
	}
	return true
}
func purchaseError(w http.ResponseWriter, err error) {
	status, message := http.StatusBadRequest, "Purchase action could not be completed; check its current state"
	switch {
	case errors.Is(err, purchase.ErrDisabled):
		status, message = http.StatusServiceUnavailable, "Purchases are not configured"
	case errors.Is(err, purchase.ErrNotFound):
		status, message = http.StatusNotFound, "Purchase not found"
	case errors.Is(err, purchase.ErrDenied):
		status, message = http.StatusForbidden, "Purchase authorization denied"
	case errors.Is(err, purchase.ErrConflict):
		status, message = http.StatusConflict, "Purchase state changed; reload the order"
	case errors.Is(err, purchase.ErrUncertain):
		status, message = http.StatusConflict, "Payment or fulfillment requires reconciliation; no replacement will be sent"
	}
	respond.Error(w, status, message)
}
func purchaseResult(w http.ResponseWriter, o purchase.Order, err error) {
	if errors.Is(err, purchase.ErrUncertain) && o.ID != "" {
		view := purchase.View(o, time.Now())
		view["reason"] = "Reconciliation is required; cancellation cannot revoke a released authorization"
		respond.JSON(w, http.StatusAccepted, view)
		return
	}
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, purchase.View(o, time.Now()))
}
func (d *Deps) purchaseCatalog(w http.ResponseWriter, r *http.Request) (purchase.Catalog, bool) {
	w.Header().Set("Cache-Control", "no-store")
	if account, _ := r.Context().Value(CtxUserID).(string); account == "" {
		respond.Error(w, http.StatusUnauthorized, "Sign in to view service offers")
		return nil, false
	}
	if d.Purchases == nil || d.Purchases.Catalog == nil {
		respond.Error(w, http.StatusServiceUnavailable, "Arkiv discovery is not configured")
		return nil, false
	}
	return d.Purchases.Catalog, true
}
func (d *Deps) PurchaseOffers(w http.ResponseWriter, r *http.Request) {
	catalog, ok := d.purchaseCatalog(w, r)
	if !ok {
		return
	}
	offers, err := catalog.List(r.Context(), r.URL.Query().Get("capability"), r.URL.Query().Get("maxPriceMicros"))
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"offers": offers})
}
func (d *Deps) PurchasePreflight(w http.ResponseWriter, r *http.Request) {
	_, ok := d.purchaseCatalog(w, r)
	if !ok {
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if !purchaseBody(w, r, &body) {
		return
	}
	if d.Purchases.ValidateEndpoint == nil || d.Purchases.ValidateEndpoint(r.Context(), body.Endpoint) != nil {
		respond.Error(w, http.StatusBadRequest, "Endpoint must resolve to a permitted public HTTPS destination")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]bool{"valid": true})
}
func (d *Deps) PurchaseWallet(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	wallet, err := s.Store.Wallet(r.Context(), account)
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, wallet)
}
func (d *Deps) PurchaseWalletChallenge(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	var body struct {
		Address string `json:"address"`
	}
	if !purchaseBody(w, r, &body) {
		return
	}
	c, err := s.ChallengeWallet(r.Context(), account, body.Address)
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"id": c.ID, "message": c.Message, "expiresAt": c.ExpiresAt})
}
func (d *Deps) PurchaseWalletLink(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	var body struct {
		ChallengeID string `json:"challengeId"`
		Signature   string `json:"signature"`
	}
	if !purchaseBody(w, r, &body) {
		return
	}
	wallet, err := s.LinkWallet(r.Context(), account, body.ChallengeID, body.Signature)
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, wallet)
}
func (d *Deps) PurchaseAllowance(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	wallet, err := s.Store.Wallet(r.Context(), account)
	if err != nil {
		purchaseError(w, err)
		return
	}
	if wallet == nil {
		respond.Error(w, http.StatusConflict, "Link a wallet first")
		return
	}
	var allowance purchase.Allowance
	if r.Method == http.MethodPost {
		var body struct {
			Limit string `json:"limitMicros"`
		}
		if !purchaseBody(w, r, &body) {
			return
		}
		allowance, err = s.Store.SetAllowance(r.Context(), account, wallet.Address, body.Limit)
	} else {
		allowance, err = s.Store.Allowance(r.Context(), account, wallet.Address)
	}
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, allowance)
}
func (d *Deps) PurchaseCreate(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	var body purchase.CreateRequest
	if !purchaseBody(w, r, &body) {
		return
	}
	o, err := s.Create(r.Context(), account, body, purchase.Invocation{})
	purchaseResult(w, o, err)
}
func (d *Deps) PurchaseGet(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	o, err := s.Store.Get(r.Context(), account, chi.URLParam(r, "purchaseId"))
	purchaseResult(w, o, err)
}
func (d *Deps) PurchaseIdentity(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	a, err := s.BeginIdentity(r.Context(), account, chi.URLParam(r, "purchaseId"))
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, a)
}
func (d *Deps) PurchaseDecision(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	var body struct {
		Intent   string `json:"intentHash"`
		Decision string `json:"decision"`
	}
	if !purchaseBody(w, r, &body) {
		return
	}
	o, err := s.Decide(r.Context(), account, chi.URLParam(r, "purchaseId"), body.Intent, body.Decision)
	purchaseResult(w, o, err)
}
func (d *Deps) PurchaseAuthorization(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	o, err := s.ReleaseAuthorization(r.Context(), account, chi.URLParam(r, "purchaseId"))
	if err != nil {
		purchaseError(w, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"order": purchase.View(o, time.Now()), "typedData": purchase.TypedData(o)})
}
func (d *Deps) PurchaseSubmit(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	var body struct {
		Signature string `json:"signature"`
	}
	if !purchaseBody(w, r, &body) {
		return
	}
	o, err := s.Submit(r.Context(), account, chi.URLParam(r, "purchaseId"), body.Signature)
	purchaseResult(w, o, err)
	d.wakePurchase(r.Context(), o)
}
func (d *Deps) PurchaseReconcile(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	o, err := s.Reconcile(r.Context(), account, chi.URLParam(r, "purchaseId"))
	purchaseResult(w, o, err)
	d.wakePurchase(r.Context(), o)
}
func (d *Deps) PurchaseCancel(w http.ResponseWriter, r *http.Request) {
	s, account, ok := d.purchaseService(w, r)
	if !ok {
		return
	}
	o, err := s.Cancel(r.Context(), account, chi.URLParam(r, "purchaseId"))
	purchaseResult(w, o, err)
}
func (d *Deps) PurchaseWorldCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if d.Purchases == nil || d.Purchases.Identity == nil {
		respond.Error(w, http.StatusServiceUnavailable, "Identity verification is not configured")
		return
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "Invalid identity callback")
		return
	}
	id, err := d.Purchases.Identity.Callback(r.Context(), query)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, "Identity verification was rejected or expired; return to the order to start again")
		return
	}
	http.Redirect(w, r, strings.TrimRight(d.FrontendURL, "/")+"/purchases?order="+url.QueryEscape(id), http.StatusSeeOther)
}

func (d *Deps) wakePurchase(ctx context.Context, o purchase.Order) {
	if o.State != purchase.Completed || o.Intent.RunID == "" || d.Engine == nil {
		return
	}
	run, err := d.Store.GetRun(ctx, o.Intent.RunID)
	if err != nil || run.Status != models.RunStatusWaiting {
		return
	}
	wf, err := d.Store.GetWorkflow(ctx, run.WorkflowID)
	if err != nil || wf.UserID != o.AccountID {
		return
	}
	wf.Nodes = decryptNodes(wf.Nodes, d.EncryptionKey)
	if _, err = d.Engine.StartResume(ctx, wf, run, false); err != nil {
		log.Printf("purchase workflow wake could not claim run %s", run.ID)
	}
}

func (d *Deps) RunPurchaseReconciler(ctx context.Context) {
	if d.Purchases == nil || d.Purchases.Service == nil || !d.Purchases.Service.Enabled {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		orders, err := d.Store.Purchases().Pending(ctx)
		if err == nil {
			for _, order := range orders {
				step, cancel := context.WithTimeout(ctx, 12*time.Second)
				o, _ := d.Purchases.Service.Reconcile(step, order.AccountID, order.ID)
				d.wakePurchase(step, o)
				cancel()
				if ctx.Err() != nil {
					return
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
