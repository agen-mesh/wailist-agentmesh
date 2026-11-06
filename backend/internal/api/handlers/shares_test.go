package handlers_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/agentmesh/backend/internal/api"
	"github.com/agentmesh/backend/internal/api/handlers"
	"github.com/agentmesh/backend/internal/arkivshare"
)

var validKey = "0x" + strings.Repeat("ab", 32)

type failOnRead struct {
	read bool
}

func (f *failOnRead) Read(p []byte) (int, error) {
	f.read = true
	return len(p), nil
}

func (f *failOnRead) Close() error { return nil }

type stubWriter struct {
	key   string
	err   error
	calls int
}

func (s *stubWriter) CreateShare(context.Context, []byte) (string, error) {
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	return s.key, nil
}

type blockingWriter struct {
	entered chan struct{}
	release chan struct{}
	key     string
}

func (b *blockingWriter) CreateShare(ctx context.Context, _ []byte) (string, error) {
	b.entered <- struct{}{}
	select {
	case <-b.release:
		return b.key, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func clearShareMeter(t *testing.T, d *handlers.Deps) {
	t.Helper()
	err := d.Store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM arkiv_share_meter`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func shareReq(body io.Reader) *http.Request {
	req := withUser(httptest.NewRequest(http.MethodPost, "/shares", body), "dev")
	req.Header.Set("Content-Type", "application/octet-stream")
	return req
}

func TestCreateShareRequiresSessionAndOctetStream(t *testing.T) {
	router := api.NewRouter(&handlers.Deps{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/shares", bytes.NewReader([]byte{1})))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session = %d, want 401", rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/shares/", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("trailing slash = %d, want 404", rec.Code)
	}
}

func TestCreateShareDoesNotReadJSON(t *testing.T) {
	body := &failOnRead{}
	req := withUser(httptest.NewRequest(http.MethodPost, "/shares", body), "dev")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	(&handlers.Deps{ShareWriter: &stubWriter{key: validKey}}).CreateShare(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType || body.read {
		t.Fatalf("code %d read %v, want 415 and unread", rec.Code, body.read)
	}

	req = withUser(httptest.NewRequest(http.MethodPost, "/shares", bytes.NewReader([]byte{1})), "dev")
	req.Header.Set("Content-Type", "application/octet-stream; charset=binary")
	rec = httptest.NewRecorder()
	(&handlers.Deps{ShareWriter: &stubWriter{key: validKey}}).CreateShare(rec, req)
	if rec.Code == http.StatusUnsupportedMediaType {
		t.Fatalf("charset parameter rejected the octet-stream body: %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateShareCountsAFailedWrite(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	d.ShareWriter = &stubWriter{err: arkivshare.ErrUnconfirmed}
	logs := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	payload := []byte("SUPER_SECRET_PROMPT")
	req := shareReq(bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	d.CreateShare(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "entityKey") || strings.Contains(rec.Body.String(), "SUPER_SECRET_PROMPT") {
		t.Fatal(rec.Body.String())
	}
	if strings.Contains(logs.String(), "SUPER_SECRET_PROMPT") {
		t.Fatal("request log contains the payload")
	}
	n, err := d.Store.ShareMeterCount(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("meter rows %d err %v, want 1", n, err)
	}
}

func TestCreateShareRejectsEmptyAndOversizedBodies(t *testing.T) {
	d := testDeps(t)
	writer := &stubWriter{key: validKey}
	d.ShareWriter = writer

	rec := httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader(nil)))
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "{\"error\":\"share_empty\"}\n" {
		t.Fatalf("empty = %d %q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader(bytes.Repeat([]byte{1}, 131073))))
	if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), "share_too_large") || writer.calls != 0 {
		t.Fatalf("oversize code %d calls %d body %s", rec.Code, writer.calls, rec.Body.String())
	}
}

func TestCreateShareDisabledWithoutAWriter(t *testing.T) {
	d := testDeps(t)
	rec := httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte{1})))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "share_disabled") {
		t.Fatalf("nil writer = %d %s", rec.Code, rec.Body.String())
	}
}

func TestCreateShareDisabledWhenTheWriterRefusesTheKey(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	d.ShareWriter = &stubWriter{err: arkivshare.ErrDisabled}
	rec := httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte{1})))
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "{\"error\":\"share_disabled\"}\n" {
		t.Fatalf("disabled writer = %d %q", rec.Code, rec.Body.String())
	}
	n, err := d.Store.ShareMeterCount(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("meter rows %d err %v, want 1", n, err)
	}
}

func TestCreateShareBusyWhenTheMeterLockIsHeld(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	started := make(chan struct{})
	release := make(chan struct{})
	holdDone := make(chan error, 1)
	go func() {
		holdDone <- d.Store.WithShareLock(context.Background(), func(ctx context.Context, tx pgx.Tx) error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	rec := httptest.NewRecorder()
	d.ShareWriter = &stubWriter{key: validKey}
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "{\"error\":\"share_busy\"}\n" {
		t.Fatalf("busy = %d %q", rec.Code, rec.Body.String())
	}
	close(release)
	if err := <-holdDone; err != nil {
		t.Fatal(err)
	}
}

func TestCreateShareDoesNotHoldTheLockWhileArkivWrites(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	writer := &blockingWriter{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		key:     validKey,
	}
	t.Cleanup(func() {
		select {
		case <-writer.release:
		default:
			close(writer.release)
		}
	})
	d.ShareWriter = writer
	firstDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
		firstDone <- rec.Code
	}()
	<-writer.entered
	secondDone := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
		secondDone <- rec.Code
	}()
	<-writer.entered
	close(writer.release)
	if code := <-firstDone; code != http.StatusCreated {
		t.Fatalf("first = %d, want 201", code)
	}
	if code := <-secondDone; code != http.StatusCreated {
		t.Fatalf("second = %d, want 201", code)
	}
}

func TestCreateShareReturnsTheReceiptAndCapsTheHour(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	d.ShareWriter = &stubWriter{key: validKey}

	rec := httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
	want := `{"entityKey":"` + validKey + `","chainId":7794095}` + "\n"
	if rec.Code != http.StatusCreated || rec.Body.String() != want {
		t.Fatalf("success = %d %q", rec.Code, rec.Body.String())
	}

	for i := 0; i < 9; i++ {
		rec = httptest.NewRecorder()
		d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %d = %d %s", i+2, rec.Code, rec.Body.String())
		}
	}
	rec = httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
	if rec.Code != http.StatusTooManyRequests || rec.Body.String() != "{\"error\":\"share_rate\"}\n" {
		t.Fatalf("11th = %d %q", rec.Code, rec.Body.String())
	}
}

func TestCreateShareCountsABadEntityKey(t *testing.T) {
	d := testDeps(t)
	clearShareMeter(t, d)
	d.ShareWriter = &stubWriter{key: "not-a-key"}
	rec := httptest.NewRecorder()
	d.CreateShare(rec, shareReq(bytes.NewReader([]byte("graph"))))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("bad key = %d %s", rec.Code, rec.Body.String())
	}
	n, err := d.Store.ShareMeterCount(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("meter rows %d err %v, want 1", n, err)
	}
}

func TestCreateShareIsBlockedWhenTheDeploymentIsReadOnly(t *testing.T) {
	t.Setenv("WEB_READONLY_MODE", "1")
	const secret = "test-secret"
	router := api.NewRouter(&handlers.Deps{JWTSecret: secret})
	req := httptest.NewRequest(http.MethodPost, "/shares", bytes.NewReader([]byte{1}))
	req.Header.Set("Authorization", "Bearer "+api.TestMakeToken(secret, "user-1"))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("readonly = %d, want 403", rec.Code)
	}
}
