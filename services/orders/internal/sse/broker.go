// Package sse provides an event broker for Server-Sent Events.
//
// Design for scale:
//   - Redis pub/sub handles cross-instance fan-out so events published on
//     any orders service replica reach clients on every other replica.
//   - An in-memory local broker handles the final delivery hop within one process.
//   - Events are stored in a Redis list (last 50 per store, 10-min TTL) so
//     reconnecting clients can replay missed events via Last-Event-ID.
//   - Falls back to in-memory-only when Redis is unavailable (local dev).
//
// Resource cost at 10k concurrent vendors:
//   ~10KB per SSE connection × 10,000 = ~100MB per instance — trivially fine.
//   Cross-instance delivery via Redis adds ~1ms latency per event publish.
package sse

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

const (
	chanPrefix = "gm:orders:events:"  // Redis pub/sub channel prefix
	histPrefix = "gm:orders:history:" // Redis list key prefix for event replay
	histLen    = 50                   // max events to keep per store
	histTTL    = 10 * time.Minute    // how long to keep event history
)

// Event is the payload pushed to SSE clients.
type Event struct {
	ID   string // millisecond Unix timestamp — used as Last-Event-ID for replay
	Type string // "order_created" | "order_updated" | "wallet_updated" | "ping"
	Data string // compact JSON payload
}

// ── In-memory local delivery broker ──────────────────────────────────────────

type memBroker struct {
	mu      sync.RWMutex
	clients map[string][]chan Event
}

func newMemBroker() *memBroker {
	return &memBroker{clients: make(map[string][]chan Event)}
}

// subscribeRW registers a new client and returns the bidirectional channel
// (used internally for replay writes) plus the unsubscribe func.
func (b *memBroker) subscribeRW(storeID string) (chan Event, func()) {
	ch := make(chan Event, 16)
	b.mu.Lock()
	b.clients[storeID] = append(b.clients[storeID], ch)
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		list := b.clients[storeID]
		for i, c := range list {
			if c == ch {
				b.clients[storeID] = append(list[:i], list[i+1:]...)
				break
			}
		}
		b.mu.Unlock()
		close(ch)
	}
}

func (b *memBroker) publish(storeID string, ev Event) {
	b.mu.RLock()
	list := b.clients[storeID]
	b.mu.RUnlock()
	for _, ch := range list {
		select {
		case ch <- ev:
		default: // client too slow — skip, don't block
		}
	}
}

// ── Redis-backed broker ───────────────────────────────────────────────────────

// Broker fans events to all SSE clients across all service replicas via Redis.
type Broker struct {
	rdb   *redis.Client // nil in in-memory fallback mode
	local *memBroker
	log   zerolog.Logger
}

// New creates a Redis-backed Broker.
// It PSUBSCRIBES to "gm:orders:events:*" so events from any service instance
// reach clients on this instance. Restarts the subscription on Redis errors.
func New(rdb *redis.Client, log zerolog.Logger) *Broker {
	b := &Broker{rdb: rdb, local: newMemBroker(), log: log}
	go b.listenRedis()
	return b
}

// NewInMemory creates a broker without Redis for local dev / single-instance.
func NewInMemory(log zerolog.Logger) *Broker {
	return &Broker{local: newMemBroker(), log: log}
}

// Publish broadcasts ev to all clients subscribed to storeID on every replica.
// Also appends ev to the Redis history list for Last-Event-ID replay.
func (b *Broker) Publish(storeID string, ev Event) {
	if ev.ID == "" {
		ev.ID = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	payload, err := json.Marshal(ev)
	if err != nil {
		b.log.Error().Err(err).Msg("sse: marshal event failed")
		return
	}

	if b.rdb == nil {
		b.local.publish(storeID, ev)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Keep a rolling history for replay (RPUSH + LTRIM + EXPIRE in one pipeline).
	histKey := histPrefix + storeID
	pipe := b.rdb.Pipeline()
	pipe.RPush(ctx, histKey, string(payload))
	pipe.LTrim(ctx, histKey, -int64(histLen), -1)
	pipe.Expire(ctx, histKey, histTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		b.log.Warn().Err(err).Str("store_id", storeID).Msg("sse: event history write failed")
	}

	// Broadcast to all replicas. The message comes back to THIS instance via
	// listenRedis → local.publish, so we must NOT also call local.publish here
	// (that would double-deliver to clients on this replica).
	if err := b.rdb.Publish(ctx, chanPrefix+storeID, string(payload)).Err(); err != nil {
		b.log.Warn().Err(err).Str("store_id", storeID).Msg("sse: redis publish failed — local delivery only")
		b.local.publish(storeID, ev)
	}
}

// Subscribe registers an SSE client for storeID and returns a receive channel
// plus an unsubscribe function the caller MUST defer.
// If lastEventID is non-empty, missed events are replayed from Redis history
// before the live stream starts.
func (b *Broker) Subscribe(ctx context.Context, storeID, lastEventID string) (<-chan Event, func()) {
	ch, unsub := b.local.subscribeRW(storeID)
	if lastEventID != "" && b.rdb != nil {
		go b.replay(ctx, storeID, lastEventID, ch)
	}
	return ch, unsub
}

// replay sends history events newer than lastEventID to ch.
func (b *Broker) replay(ctx context.Context, storeID, lastEventID string, ch chan Event) {
	rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	items, err := b.rdb.LRange(rctx, histPrefix+storeID, 0, -1).Result()
	if err != nil {
		b.log.Warn().Err(err).Str("store_id", storeID).Msg("sse: history fetch failed")
		return
	}

	lastTS, _ := strconv.ParseInt(lastEventID, 10, 64)
	for _, item := range items {
		var ev Event
		if json.Unmarshal([]byte(item), &ev) != nil {
			continue
		}
		ts, _ := strconv.ParseInt(ev.ID, 10, 64)
		if ts <= lastTS {
			continue
		}
		select {
		case ch <- ev:
		case <-ctx.Done():
			return
		default:
		}
	}
}

// listenRedis PSUBSCRIBES to all order event channels and delivers messages
// to local clients. Automatically restarts on Redis errors.
func (b *Broker) listenRedis() {
	for {
		b.runRedisSubscription()
		b.log.Warn().Msg("sse: redis subscription ended — restarting in 2s")
		time.Sleep(2 * time.Second)
	}
}

func (b *Broker) runRedisSubscription() {
	ctx := context.Background()
	pubsub := b.rdb.PSubscribe(ctx, chanPrefix+"*")
	defer pubsub.Close()
	b.log.Info().Msg("sse: listening on redis order event channels")

	for msg := range pubsub.Channel() {
		storeID := strings.TrimPrefix(msg.Channel, chanPrefix)
		var ev Event
		if json.Unmarshal([]byte(msg.Payload), &ev) != nil {
			continue
		}
		b.local.publish(storeID, ev)
	}
}

// Ping sends a heartbeat to all local subscribers of storeID (not via Redis —
// heartbeats are per-instance and don't need cross-instance delivery).
func (b *Broker) Ping(storeID string) {
	b.local.publish(storeID, Event{
		ID:   strconv.FormatInt(time.Now().UnixMilli(), 10),
		Type: "ping",
		Data: fmt.Sprintf(`{"ts":%d}`, time.Now().UnixMilli()),
	})
}
