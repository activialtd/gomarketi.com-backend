// Package sse provides an in-memory pub/sub broker for Server-Sent Events.
// One broker instance lives per orders service process.
// Events are fan-out: all clients subscribed to a storeID receive every event.
package sse

import (
	"fmt"
	"sync"
)

// Event is the payload pushed to SSE clients.
type Event struct {
	Type string // e.g. "order_created", "order_updated", "wallet_updated"
	Data string // JSON string or simple value
}

// Broker fans events out to all SSE clients subscribed to a given store ID.
type Broker struct {
	mu      sync.RWMutex
	clients map[string][]chan Event // storeID → subscriber channels
}

// New returns a ready-to-use Broker.
func New() *Broker {
	return &Broker{clients: make(map[string][]chan Event)}
}

// Subscribe registers a new SSE client for storeID.
// Returns a channel that receives events and an unsubscribe function the
// caller MUST invoke when the client disconnects (e.g. via defer).
func (b *Broker) Subscribe(storeID string) (<-chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.clients[storeID] = append(b.clients[storeID], ch)
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		list := b.clients[storeID]
		for i, c := range list {
			if c == ch {
				b.clients[storeID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		close(ch)
	}
}

// Publish sends an event to all clients subscribed to storeID.
// Non-blocking: slow/disconnected clients are skipped.
func (b *Broker) Publish(storeID string, ev Event) {
	b.mu.RLock()
	list := b.clients[storeID]
	b.mu.RUnlock()
	for _, ch := range list {
		select {
		case ch <- ev:
		default: // client too slow — skip rather than block
		}
	}
}

// Ping sends a no-op heartbeat to all subscribers of storeID.
func (b *Broker) Ping(storeID string) {
	b.Publish(storeID, Event{Type: "ping", Data: fmt.Sprintf(`{"store_id":%q}`, storeID)})
}
