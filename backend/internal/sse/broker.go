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
	done    chan struct{}
	closed  bool
}

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
	ch := make(chan models.LogEvent, 32)
	h.mu.Lock()
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
	h.mu.RLock()
	defer h.mu.RUnlock()
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
