package sse

import (
	"sync"

	"github.com/agentmesh/backend/internal/models"
)

type Broker struct {
	mu   sync.Mutex
	hubs map[string]*hub
}

type hub struct {
	mu      sync.RWMutex
	clients map[chan models.LogEvent]struct{}
	// history is every event published for this run so far, replayed to each
	// new subscriber. Without it, Publish's non-blocking send reaches only
	// whoever is already attached, and an event published before the browser
	// finishes opening its EventSource is lost with no way to recover it.
	// That is a guaranteed race, not a rare one: the first node's event is
	// typically published a few hundred ms after the run starts, while the
	// client only learns the run id from the POST response and connects
	// after that (confirmed live 2026-08-02 — a 35s stream that carried a
	// keepalive and not one event).
	history []models.LogEvent
	done    chan struct{}
	closed  bool
}

// maxHistory bounds per-run replay memory. Runs are short-lived and a step
// emits one event, so this is far above any real workflow while still
// refusing to grow without limit on a pathological run.
const maxHistory = 500

func NewBroker() *Broker {
	return &Broker{hubs: make(map[string]*hub)}
}

func (b *Broker) Create(runID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hubs[runID] = &hub{
		clients: make(map[chan models.LogEvent]struct{}),
		done:    make(chan struct{}),
	}
}

func (b *Broker) Subscribe(runID string) (chan models.LogEvent, func()) {
	b.mu.Lock()
	h, ok := b.hubs[runID]
	b.mu.Unlock()
	if !ok {
		ch := make(chan models.LogEvent)
		return ch, func() { close(ch) }
	}
	h.mu.Lock()
	// Size the buffer to fit the replay plus headroom, so seeding it below
	// cannot block and cannot drop the very events it exists to recover.
	ch := make(chan models.LogEvent, len(h.history)+32)
	for _, ev := range h.history {
		ch <- ev
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}
}

func (b *Broker) Publish(runID string, ev models.LogEvent) {
	b.mu.Lock()
	h, ok := b.hubs[runID]
	b.mu.Unlock()
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.history) < maxHistory {
		h.history = append(h.history, ev)
	}
	for ch := range h.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (b *Broker) Close(runID string) {
	b.mu.Lock()
	h, ok := b.hubs[runID]
	b.mu.Unlock()
	if !ok {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		close(h.done)
	}
}

func (b *Broker) Done(runID string) <-chan struct{} {
	b.mu.Lock()
	h, ok := b.hubs[runID]
	b.mu.Unlock()
	if !ok {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return h.done
}
