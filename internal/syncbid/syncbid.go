package syncbid

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const stream = "bids_stream"

// Run tails the Redis stream and persists every bid.
func Run(ctx context.Context, rdc *redis.Client, db *sql.DB) {
	go func() {
		lastID := "0-0"
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// block up to 2 s for new entries
			res, err := rdc.XRead(ctx, &redis.XReadArgs{
				Streams: []string{stream, lastID},
				Count:   100,
				Block:   2000 * time.Millisecond,
			}).Result()
			if err != nil && err != redis.Nil {
				zap.L().Warn("syncbid.xread", zap.Error(err))
				time.Sleep(time.Second)
				continue
			}
			if len(res) == 0 {
				continue
			}
			entries := res[0].Messages
			if err := persist(ctx, db, entries); err != nil {
				// zap.L().Error("syncbid.persist", zap.Error(err))
			}
			lastID = entries[len(entries)-1].ID
		}
	}()
}

func persist(ctx context.Context, db *sql.DB, msgs []redis.XMessage) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	const ins = `INSERT INTO bids (auction_id, bidder_id, amount, placed_at)
	             VALUES ($1, $2, $3, to_timestamp($4))
	             ON CONFLICT DO NOTHING`
	for _, m := range msgs {
		aid := m.Values["aid"].(string)
		bidder := m.Values["bidder"].(string)
		amt := m.Values["amount"].(string)
		at := m.Values["at"].(string)

		// zap.L().Debug("persist_bid",
		// 	zap.String("aid", aid),
		// 	zap.String("bidder", bidder),
		// 	zap.String("amount", amt),
		// )
		amount, _ := strconv.ParseFloat(amt, 64)
		ts, _ := strconv.ParseInt(at, 10, 64)
		if _, err := tx.ExecContext(ctx, ins, aid, bidder, amount, ts); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
