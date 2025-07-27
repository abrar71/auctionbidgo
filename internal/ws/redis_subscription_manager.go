package ws

import (
	"context"
	"sync"

	"github.com/redis/go-redis/v9"
)

// subscriptionManager guarantees that we have **exactly one** Redis
// subscription per “auc:<id>:events” channel — no matter how many websocket
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

	// first consumer → create Redis SUB and fan‑out loop
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
				if !ok {
					return
				}
				sm.hub.Broadcast(auctionID, []byte(m.Payload))
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

	// outside the lock → stop the fan‑out goroutine
	e.cancel()
}
