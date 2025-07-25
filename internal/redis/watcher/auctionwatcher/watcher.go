package auctionwatcher

import (
	"auctionbidgo/internal/services/auction"
	"context"
	"strings"

	"github.com/redis/go-redis/v9"
)

// Run listens to key‑expiry events and finalises auctions in Postgres.
// Run must be started once at service boot.
func Run(ctx context.Context, rdb *redis.Client, svc auction.IAuctionService) {
	_ = rdb.ConfigSet(ctx, "notify-keyspace-events", "Ex").Err()
	ps := rdb.PSubscribe(ctx, "__keyevent@*__:expired")
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-ps.Channel():
			if !strings.HasPrefix(m.Payload, "auc_t:") {
				continue
			}
			id := strings.TrimPrefix(m.Payload, "auc_t:")
			_ = svc.Finalize(ctx, id) // errors already logged in svc
		}
	}
}
