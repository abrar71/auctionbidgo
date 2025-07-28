package syncdb

import (
	"context"
	"database/sql"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	activeSet   = "aucs:active"
	hashPrefix  = "auc:"
	pipeTimeout = 1500 * time.Millisecond
)

// Every 10 s, mirror "active" auctions' high bid -> Postgres.
func Run(ctx context.Context, rdc *redis.Client, db *sql.DB) {
	tk := time.NewTicker(10 * time.Second)
	go func() {
		defer tk.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tk.C:
				syncOnce(ctx, rdc, db)
			}
		}
	}()
}

func syncOnce(ctx context.Context, rdc *redis.Client, db *sql.DB) {
	keys, err := rdc.SMembers(ctx, activeSet).Result()
	if err != nil || len(keys) == 0 {
		return
	}

	// 1. fetch all hashes in one pipelined round‑trip
	pipe := rdc.Pipeline()
	cmds := make([]*redis.MapStringStringCmd, len(keys))
	for i, k := range keys {
		cmds[i] = pipe.HGetAll(ctx, k)
	}

	if _, err = pipe.Exec(ctx); err != nil {
		zap.L().Error("syncdb.pipeline", zap.Error(err))
		return
	}

	// 2. bulk‑upsert into Postgres
	const upsert = `
	INSERT INTO auctions (id, seller_id, item, starts_at, ends_at,
	                      status, high_bid, high_bidder)
	     VALUES ($1,$2,'',to_timestamp($3),to_timestamp($4),
	             'RUNNING',$5,$6)
	ON CONFLICT (id) DO UPDATE
	       SET high_bid=EXCLUDED.high_bid,
	           high_bidder=EXCLUDED.high_bidder`

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		zap.L().Error("syncdb.tx_begin", zap.Error(err))
		return
	}
	defer tx.Rollback()

	for i, cmd := range cmds {
		data, err := cmd.Result()
		if err != nil || len(data) == 0 {
			continue // key disappeared between SMEMBERS and HGETALL
		}
		id := keys[i][len(hashPrefix):] // strip "auc:"
		if _, err := tx.ExecContext(ctx, upsert,
			id, data["sid"], data["sa"], data["ea"], data["hb"], data["hbid"]); err != nil {
			zap.L().Error("syncdb.upsert", zap.String("id", id), zap.Error(err))
		}
	}

	err = tx.Commit()
	if err != nil {
		zap.L().Debug("syncdb_error", zap.Error(err))
	}
}
