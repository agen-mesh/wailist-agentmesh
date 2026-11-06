package merchant

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestLifecyclePaymentDeadlineUsesBlockTime(t *testing.T) {
	for _, delta := range []time.Duration{-time.Second, 0, time.Second} {
		t.Run(delta.String(), func(t *testing.T) {
			f := newFixture(t)
			f.accept(t)
			f.paidAt = f.snap.Deadline.Add(delta)
			f.now = f.snap.Deadline.Add(24 * time.Hour)
			r, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event)
			if delta < 0 {
				if err != nil || r.State != Completed || f.calls.Load() != 1 {
					t.Fatal("valid earlier block payment rejected", err)
				}
			} else if !errors.Is(err, ErrPayment) || f.calls.Load() != 0 {
				t.Fatal("expired block payment accepted", err)
			}
		})
	}
}

func TestLifecycleVerificationCannotChangeAcceptedRequest(t *testing.T) {
	f := newFixture(t)
	f.accept(t)
	f.svc.verify = func(ctx context.Context, s Snapshot, e PaymentEvent) (PaymentProof, error) {
		p, _ := f.verify(ctx, s, e)
		s.Request[0] = '['
		s.CanonicalIntent[0] = '['
		return p, nil
	}
	r, err := f.svc.Fulfill(context.Background(), f.snap.OrderID, f.snap.IntentHash, f.event)
	if err != nil || r.State != Completed {
		t.Fatal("verification mutated durable terms", err)
	}
	stored, err := f.store.Get(context.Background(), f.snap.OrderID)
	if err != nil || !bytes.Equal(stored.Snapshot.Request, f.snap.Request) {
		t.Fatal("stored request mutated", err)
	}
}

func TestLifecycleUncertainLookupWithholdsPartialResult(t *testing.T) {
	f := newFixture(t)
	f.accept(t)
	f.store.mu.Lock()
	r := f.store.rows[f.snap.OrderID]
	r.State = Unknown
	r.Payment = &f.event
	r.Result = []byte(`{"partial":"not a completed result"}`)
	f.store.rows[f.snap.OrderID] = r
	f.store.mu.Unlock()
	got, err := f.svc.Result(context.Background(), f.snap.OrderID, f.snap.IntentHash)
	if err != nil || got.State != Unknown || len(got.Result) != 0 {
		t.Fatal("unknown lookup exposed incomplete output", err)
	}
	if view := View(r); len(view.Result) != 0 || !view.Paid || !view.ReconciliationRequired {
		t.Fatal("unknown HTTP view misrepresents result")
	}
}
