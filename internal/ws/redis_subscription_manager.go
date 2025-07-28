package ws

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// subscriptionManager guarantees that we have **exactly one** Redis
// subscription per "auc:<id>:events" channel ― no matter how many websocket
// clients join the same auction room.
type subscriptionManager struct {
	rdb  *redis.Client
	hub  *Hub
	mu   sync.Mutex
	subs map[string]*subEntry // auctionID ➜ subscription data
}

type subEntry struct {
	refCnt int
	cancel context.CancelFunc
}

func newSubscriptionManager(rdb *redis.Client, hub *Hub) *subscriptionManager {
	return &subscriptionManager{
		rdb:  rdb,
		hub:  hub,
		subs: make(map[string]*subEntry),
	}
}

// Subscribe ensures that the process is subscribed to the auction’s channel;
// subsequent calls for the same auction only increment the ref‑counter.
func (sm *subscriptionManager) Subscribe(auctionID string) {
	sm.mu.Lock()
	if e, ok := sm.subs[auctionID]; ok {
		e.refCnt++
		sm.mu.Unlock()
		return
	}

	// First consumer → create Redis SUB and fan‑out loop.
	ctx, cancel := context.WithCancel(context.Background())
	ps := sm.rdb.Subscribe(ctx, "auc:"+auctionID+":events")

	sm.subs[auctionID] = &subEntry{refCnt: 1, cancel: cancel}
	sm.mu.Unlock()

	go func() {
		defer ps.Close()

		for {
			select {
			case <-ctx.Done():
				return
			case m, ok := <-ps.Channel():
				if !ok { // Redis connection closed.
					return
				}

				// Wrap the raw Redis payload into the public WS envelope so
				// that **all** messages (server‑initiated *and* client‑initiated)
				// respect the same router contract format.
				wrapped, err := wrapRedisEvent(m.Payload)
				if err != nil {
					zap.L().Warn("ws.wrap_event_failed", zap.Error(err))
					wrapped = []byte(m.Payload) // Fallback: forward as‑is.
				}

				sm.hub.Broadcast(auctionID, wrapped)
			}
		}
	}()
}

// Unsubscribe decrements the ref‑counter and tears the Redis SUB down when the
// last websocket client leaves the room.
func (sm *subscriptionManager) Unsubscribe(auctionID string) {
	sm.mu.Lock()
	e, ok := sm.subs[auctionID]
	if !ok {
		sm.mu.Unlock()
		return
	}
	e.refCnt--
	if e.refCnt > 0 {
		sm.mu.Unlock()
		return
	}
	delete(sm.subs, auctionID)
	sm.mu.Unlock()

	// Outside the lock → stop the fan‑out goroutine.
	e.cancel()
}

// ─────────────────────────────── helpers ─────────────────────────────────────

// wrapRedisEvent turns
//
//	{"version":1,"event":"bid","bidder":"u1",…}
//
// into
//
//	{"event":"auctions/bid","body":{"version":1,"bidder":"u1",…}}
func wrapRedisEvent(payload string) ([]byte, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return nil, err
	}

	evt, _ := raw["event"].(string)
	if evt == "" {
		evt = "unknown"
	}
	delete(raw, "event") // Avoid duplication inside “body”.

	env := map[string]interface{}{
		"event": "auctions/" + evt,
		"body":  raw,
	}
	return json.Marshal(env)
}
