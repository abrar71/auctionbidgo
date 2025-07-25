package syncdb

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Every 10 s, mirror "active" auctions' high bid -> Postgres.
func Run(ctx context.Context, rdc *redis.Client, db *sql.DB) {
	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				syncOnce(ctx, rdc, db)
			}
		}
	}()
}

func syncOnce(ctx context.Context, rdc *redis.Client, db *sql.DB) {
	keys, err := rdc.SMembers(ctx, "aucs:active").Result()
	if err != nil {
		zap.L().Error("syncdb.smembers", zap.Error(err))
		return
	}
	for _, key := range keys {
		data, err := rdc.HGetAll(ctx, key).Result()
		if err != nil || len(data) == 0 {
			continue
		}
		id := key[len("auc:"):]
		const upsert = `
		  INSERT INTO auctions (id, seller_id, item, starts_at, ends_at,
		                        status, high_bid, high_bidder)
		       VALUES ($1, $2, '', to_timestamp($3), to_timestamp($4),
		               'RUNNING', $5, $6)
		  ON CONFLICT (id) DO UPDATE
		        SET high_bid    = EXCLUDED.high_bid,
		            high_bidder = EXCLUDED.high_bidder`
		if _, err = db.ExecContext(ctx, upsert,
			id,
			data["sid"],
			data["sa"],
			data["ea"],
			data["hb"],
			data["hbid"],
		); err != nil {
			zap.L().Error("syncdb.upsert", zap.String("id", id), zap.Error(err))
		}
	}
}
