package ws

import (
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

// SubscribeRedisAuctionEvents fans‑out messages coming from any instance
// to the in‑process Hub.
func SubscribeRedisAuctionEvents(ctx context.Context, rdb *redis.Client, hub *Hub) {
	pubsub := rdb.PSubscribe(ctx, "auc:*:events")
	defer pubsub.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case m, ok := <-pubsub.Channel():
			if !ok {
				return
			}
			// channel format: "auc:<auctionID>:events"
			parts := strings.Split(m.Channel, ":")
			if len(parts) != 3 {
				continue
			}
			hub.Broadcast(parts[1], []byte(m.Payload)) // ← correct auction ID
		}
	}
}
