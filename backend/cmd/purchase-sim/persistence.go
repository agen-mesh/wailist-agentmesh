package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"github.com/agentmesh/backend/internal/purchasesim"
)

func writePrivate(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".private-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func restoreFixture(f *purchasesim.Fixture, path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return errors.New("unsafe private chain state")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return f.RestoreState(raw)
}

func (a *simulationApp) mutateFixture(fn func() error) error {
	a.chainMu.Lock()
	defer a.chainMu.Unlock()
	mutationErr := fn()
	raw, err := a.fixture.MarshalState()
	if err != nil {
		return err
	}
	if err = writePrivate(a.chainFile, raw); err != nil {
		return err
	}
	return mutationErr
}

// Flush private chain state before acknowledging any simulated RPC response.
func (a *simulationApp) fixtureHTTP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		if err := a.mutateFixture(func() error { next.ServeHTTP(recorder, r); return nil }); err != nil {
			http.Error(w, "private fixture persistence unavailable", http.StatusServiceUnavailable)
			return
		}
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	})
}
