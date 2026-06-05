package sse_test

import (
	"testing"
	"time"

	"github.com/agentmesh/backend/internal/models"
	"github.com/agentmesh/backend/internal/sse"
)

func TestBrokerPublishSubscribe(t *testing.T) {
	b := sse.NewBroker()
	runID := "test-run-1"
	b.Create(runID)

	ch, unsub := b.Subscribe(runID)
	defer unsub()

	ev := models.LogEvent{NodeID: "n1", Status: models.LogStatusSuccess}
	b.Publish(runID, ev)

	select {
	case got := <-ch:
		if got.NodeID != "n1" {
			t.Fatalf("want n1 got %s", got.NodeID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}

	b.Close(runID)
	select {
	case <-b.Done(runID):
		// ok
	case <-time.After(time.Second):
		t.Fatal("done channel not closed")
	}
}
