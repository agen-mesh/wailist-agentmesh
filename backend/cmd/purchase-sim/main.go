package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/arc"
	"github.com/agentmesh/backend/internal/arkiv"
	"github.com/agentmesh/backend/internal/db"
	"github.com/agentmesh/backend/internal/engine"
	"github.com/agentmesh/backend/internal/merchant"
	"github.com/agentmesh/backend/internal/purchase"
	"github.com/agentmesh/backend/internal/purchasesim"
	"github.com/agentmesh/backend/internal/sse"
	"github.com/agentmesh/backend/internal/wallet"
	"github.com/agentmesh/backend/internal/world"
	worldsim "github.com/agentmesh/backend/internal/world/simulation"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

const simulationResource = "https://merchant.simulation.invalid/tool"

var errRestart = errors.New("controlled simulation restart")

type privateState struct {
	Encryption string
	JWT        string
	Relayer    string
	Provider   string
	Merchant   string
	Wallets    map[string]string
	Subject    string
}

type simulationApp struct {
	mu            sync.Mutex
	chainMu       sync.Mutex
	state         privateState
	stateFile     string
	chainFile     string
	fixture       *purchasesim.Fixture
	issuer        *worldsim.Issuer
	rail          *purchase.ArcRail
	store         *db.Store
	pool          *pgxpool.Pool
	frontend      string
	merchantFault string
	counters      map[string]int
	restart       chan struct{}
}

func main() {
	if err := run(); err != nil {
		if errors.Is(err, errRestart) {
			if executable, lookupErr := os.Executable(); lookupErr == nil {
				_ = syscall.Exec(executable, os.Args, os.Environ())
			}
		}
		log.Fatal("controlled simulation startup or runtime failed; check the loopback scratch configuration")
	}
}

func loopbackAddress(raw string) bool {
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func run() error {
	listen := flag.String("listen", "127.0.0.1:8104", "loopback application listener")
	frontend := flag.String("frontend", "http://127.0.0.1:3104", "loopback frontend origin")
	directory := flag.String("state-dir", "", "owned private directory retained for restart checks")
	flag.Parse()
	front, err := url.Parse(*frontend)
	if err != nil || front.Scheme != "http" || !loopbackAddress(front.Host) || front.Path != "" || front.RawQuery != "" || !loopbackAddress(*listen) || *directory == "" {
		return errors.New("invalid simulation boundaries")
	}
	databaseURL := os.Getenv("PURCHASE_SIM_DATABASE_URL")
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil || !net.ParseIP(cfg.ConnConfig.Host).IsLoopback() || !strings.HasPrefix(cfg.ConnConfig.Database, "ao") {
		return errors.New("owned loopback scratch database required")
	}
	info, err := os.Lstat(*directory)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		return errors.New("private state directory required")
	}
	stateFile := filepath.Join(*directory, "private-state.json")
	state, err := loadState(stateFile)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	store, err := db.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer pool.Close()
	provider, err := crypto.HexToECDSA(state.Provider)
	if err != nil {
		return err
	}
	payee := strings.ToLower(crypto.PubkeyToAddress(provider.PublicKey).Hex())
	fixture, err := purchasesim.New(purchasesim.Config{Resource: simulationResource, Payee: payee, PriceMicros: "10000"})
	if err != nil {
		return err
	}
	chainFile := filepath.Join(*directory, "chain-state.json")
	if err = restoreFixture(fixture, chainFile); err != nil {
		return err
	}
	base := "http://" + *listen
	issuer, err := worldsim.New(base + "/purchases/world/callback")
	if err != nil {
		return err
	}
	defer issuer.Close()
	app := &simulationApp{state: state, stateFile: stateFile, chainFile: chainFile, fixture: fixture, issuer: issuer, store: store, pool: pool, frontend: *frontend, counters: map[string]int{}, restart: make(chan struct{}, 1)}
	if app.state.Subject == "" {
		app.state.Subject = "scenario-" + state.JWT[:24]
	}
	if err = app.persistState(); err != nil {
		return err
	}
	if err = issuer.SetSubject(app.state.Subject); err != nil {
		return err
	}
	arcServer := httptest.NewTLSServer(app.fixtureHTTP(fixture.ArcHandler()))
	defer arcServer.Close()
	arkivServer := httptest.NewTLSServer(app.fixtureHTTP(fixture.ArkivHandler()))
	defer arkivServer.Close()
	policy := func(_ context.Context, target string) error {
		if target != simulationResource {
			return purchase.ErrDenied
		}
		return nil
	}
	catalog, err := arkiv.NewClientWithHTTP(arkivServer.URL, policy, arkivServer.Client())
	if err != nil {
		return err
	}
	relayer, err := crypto.HexToECDSA(state.Relayer)
	if err != nil {
		return err
	}
	rail := &purchase.ArcRail{Caller: arc.HTTPCaller{HTTP: arcServer.Client()}, RPC: arcServer.URL, Key: relayer, MaxGasWei: big.NewInt(1000000000000000), Confirmations: 2}
	app.rail = rail
	private, err := hex.DecodeString(state.Merchant)
	if err != nil {
		return err
	}
	public := ed25519.PrivateKey(private).Public().(ed25519.PublicKey)
	merchantStore := &controlledMerchantStore{Store: store.MerchantPurchases(), app: app}
	merchantService, err := merchant.New(merchant.Config{Resource: simulationResource, Payee: payee, AmountMicros: "10000", Capability: merchant.Capability, PrivateKey: private, Execute: app.execute}, merchantStore, rail.MerchantVerifier)
	if err != nil {
		return err
	}
	merchantHandler, err := merchant.NewHandler(merchantService, state.JWT)
	if err != nil {
		return err
	}
	merchantServer := httptest.NewTLSServer(app.observeMerchant(merchantHandler))
	defer merchantServer.Close()
	merchantClient := &purchase.MerchantClient{Client: merchantServer.Client(), Policy: policy, BaseURL: merchantServer.URL + "/merchant", Resource: simulationResource, Bearer: state.JWT, PublicKey: public, Payee: payee, PriceMicros: "10000"}
	registration := issuer.Registration()
	identityStore := store.PurchaseIdentities(state.Encryption)
	worldHTTP := issuer.HTTPClient()
	worldHTTP.Transport = countingTransport{base: worldHTTP.Transport, app: app}
	worldClient, err := world.New(ctx, world.Config{Enabled: true, Environment: world.Sandbox, Issuer: registration.Issuer, ClientID: registration.ClientID, ClientSecret: registration.ClientSecret, RedirectURI: registration.RedirectURI}, identityStore, worldHTTP)
	if err != nil {
		return err
	}
	identity := &purchase.WorldIdentity{Client: worldClient, Store: identityStore, Issuer: registration.Issuer, AuthorizationURL: issuer.BrowserURL}
	service := &purchase.Service{Enabled: true, Store: store.Purchases(), Catalog: purchase.ArkivCatalog{Client: catalog}, Merchant: merchantClient, Identity: identity, Rail: rail, Domain: *frontend, Seal: func(s string) (string, error) { return wallet.Encrypt(s, state.Encryption) }, Open: func(s string) (string, error) { return wallet.Decrypt(s, state.Encryption) }}
	broker := sse.NewBroker()
	runner := engine.NewRunner(store, broker, nil, base, "", "", engine.X402Config{}, arc.Config{})
	runner.SetPurchases(service)
	deps := &handlers.Deps{Store: store, Broker: broker, Engine: runner, ArcConfig: &arc.Config{}, BaseURL: base, FrontendURL: *frontend, JWTSecret: state.JWT, EncryptionKey: state.Encryption, Purchases: &handlers.PurchaseRuntime{Service: service, Catalog: service.Catalog, Identity: identity, ValidateEndpoint: policy, PublicConfig: map[string]any{"enabled": true, "blockedReasons": []string{}, "chainId": purchase.ChainID, "asset": purchase.Asset, "rpcUrl": base + "/__verify__/arc", "merchantCapability": merchant.Capability, "arkivAvailable": true, "worldAvailable": true, "evidenceMode": "simulation"}}}
	router := api.NewRouter(deps)
	mux := http.NewServeMux()
	mux.Handle("/__verify__/", app.controls())
	mux.Handle("/", router)
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); deps.RunPurchaseReconciler(ctx) }()
	errors := make(chan error, 1)
	go func() { errors <- server.ListenAndServe() }()
	log.Printf("Controlled simulation only: app %s, frontend %s, no public network payments", base, *frontend)
	requestedRestart := false
	select {
	case <-ctx.Done():
	case <-app.restart:
		requestedRestart = true
	case err = <-errors:
		if err != nil && err != http.ErrServerClosed {
			cancel()
		}
	}
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = server.Shutdown(shutdown)
	cancel()
	workers.Wait()
	if requestedRestart {
		if persistErr := app.mutateFixture(func() error { return nil }); persistErr != nil {
			return persistErr
		}
		return errRestart
	}
	return err
}

func loadState(path string) (privateState, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return privateState{}, fmt.Errorf("unsafe private state file")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return privateState{}, err
		}
		var state privateState
		err = json.Unmarshal(raw, &state)
		if err != nil || len(state.Encryption) != 32 || state.Wallets == nil {
			return privateState{}, fmt.Errorf("invalid private state")
		}
		return state, nil
	} else if !os.IsNotExist(err) {
		return privateState{}, err
	}
	random := func(size int) (string, error) {
		raw := make([]byte, size)
		_, err := rand.Read(raw)
		return hex.EncodeToString(raw), err
	}
	encryption, err := random(16)
	if err != nil {
		return privateState{}, err
	}
	jwt, err := random(32)
	if err != nil {
		return privateState{}, err
	}
	relayer, err := crypto.GenerateKey()
	if err != nil {
		return privateState{}, err
	}
	provider, err := crypto.GenerateKey()
	if err != nil {
		return privateState{}, err
	}
	_, merchantKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return privateState{}, err
	}
	s := privateState{Encryption: encryption, JWT: jwt, Relayer: hex.EncodeToString(crypto.FromECDSA(relayer)), Provider: hex.EncodeToString(crypto.FromECDSA(provider)), Merchant: hex.EncodeToString(merchantKey), Wallets: map[string]string{}}
	raw, err := json.Marshal(s)
	if err != nil {
		return s, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return s, err
	}
	_, err = file.Write(raw)
	closeErr := file.Close()
	if err != nil {
		return s, err
	}
	return s, closeErr
}

type countingTransport struct {
	base http.RoundTripper
	app  *simulationApp
}

func (t countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/token" {
		t.app.count("world.token")
	}
	return t.base.RoundTrip(r)
}
func (a *simulationApp) count(key string) { a.mu.Lock(); defer a.mu.Unlock(); a.counters[key]++ }
