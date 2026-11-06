package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentmesh/backend/internal/purchasesim"
)

func TestPrivateFixtureRestoresFreshInstanceAndRejectsUnsafeState(t *testing.T) {
	config := purchasesim.Config{Resource: simulationResource, Payee: "0x1111111111111111111111111111111111111111", PriceMicros: "10000"}
	f, err := purchasesim.New(config)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "chain.json")
	app := &simulationApp{fixture: f, chainFile: path}
	if err = app.mutateFixture(func() error { return f.AdvanceBlocks(7, 13) }); err != nil {
		t.Fatal(err)
	}
	restarted, err := purchasesim.New(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = restoreFixture(restarted, path); err != nil {
		t.Fatal(err)
	}
	query := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`)
	read := func(f *purchasesim.Fixture) string {
		w := httptest.NewRecorder()
		f.ArcHandler().ServeHTTP(w, httptest.NewRequest("POST", "/", bytes.NewReader(query)))
		return w.Body.String()
	}
	if read(f) != read(restarted) {
		t.Fatal("restart changed confirmed chain head")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if restoreFixture(restarted, path) == nil {
		t.Fatal("publicly readable state accepted")
	}
}

func TestFixtureDoesNotAcknowledgeUnpersistedRPC(t *testing.T) {
	f, err := purchasesim.New(purchasesim.Config{Resource: simulationResource, Payee: "0x1111111111111111111111111111111111111111", PriceMicros: "10000"})
	if err != nil {
		t.Fatal(err)
	}
	app := &simulationApp{fixture: f, chainFile: filepath.Join(t.TempDir(), "absent", "chain.json")}
	w := httptest.NewRecorder()
	app.fixtureHTTP(f.ArcHandler()).ServeHTTP(w, httptest.NewRequest("POST", "/", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`)))
	if w.Code != 503 {
		t.Fatalf("acknowledged unavailable persistence: %d", w.Code)
	}
}
