package auction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type AuctionDTO struct {
	ID         string    `json:"id"`
	SellerID   string    `json:"seller_id"`
	StartsAt   time.Time `json:"starts_at" example:"2025-07-27T16:05:05Z"`
	EndsAt     time.Time `json:"ends_at"   example:"2025-07-27T16:05:05Z"`
	Status     string    `json:"status"    example:"RUNNING"`
	HighBid    float64   `json:"high_bid"`
	HighBidder string    `json:"high_bidder"`
}

const (
	redisAuctionKeyPrefix      = "auc:"
	redisAuctionTimerKeyPrefix = "auc_t:"
)

var (
	ErrAuctionClosed     = errors.New("auction closed")
	ErrBidEqual          = errors.New("bid must be higher than current bid")
	ErrBidBelowIncrement = errors.New("bid below min increment")
	ErrBidBelowCurrent   = errors.New("bid below current high bid")

	ErrAlreadyRunning  = errors.New("auction already running")
	ErrAuctionFinished = errors.New("auction already finished")
	ErrAuctionExists   = errors.New("auction already exists")
	ErrAuctionRunning  = errors.New("auction is running, delete forbidden")
)

type IAuctionService interface {
	CreateAuction(ctx context.Context, id, sellerID, item string, endsAt time.Time) (string, error)
	StartAuction(ctx context.Context, auctionID, sellerID string, endsAt time.Time) error
	StopAuction(ctx context.Context, auctionId string) error
	PlaceBid(ctx context.Context, auctionId string, userId string, bidAmount float64) error
	Finalize(ctx context.Context, auctionId string) error
	GetAuction(ctx context.Context, id string) (*AuctionDTO, error)
	ListAuctions(ctx context.Context, status string, limit, offset int) ([]AuctionDTO, error)
	DeleteAuction(ctx context.Context, id string) error
}

type auctionService struct {
	rdc          *redis.Client
	db           *sql.DB
	minIncrement float64
}

var _ = (*auctionService)(nil)

func NewAuctionService(rdc *redis.Client, db *sql.DB, minInc float64) IAuctionService {
	return &auctionService{
		rdc:          rdc,
		db:           db,
		minIncrement: minInc,
	}
}

// CreateAuction persists a row in Postgres in *PENDING* state.
//   - If `id` is empty a random UUID is generated.
//   - It fails when an auction with the same ID already exists
//     (whatever its state).
func (svc *auctionService) CreateAuction(
	ctx context.Context, id, sellerID, item string, endsAt time.Time,
) (string, error) {
	if id == "" {
		id = uuid.NewString()
	}

	// safety – keep max 24 h gap between draft creation and start
	if endsAt.Before(time.Now().Add(30 * time.Second)) {
		return "", ErrAuctionClosed
	}

	const q = `
      INSERT INTO auctions (id, seller_id, item,
                            starts_at, ends_at, status)
           VALUES ($1, $2, $3, now(), $4, 'PENDING')`
	if _, err := svc.db.ExecContext(ctx, q,
		id, sellerID, item, endsAt); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return "", ErrAuctionExists
		}
		return "", err
	}
	return id, nil
}

// Start creates the disposable Redis hash + TTL
func (svc *auctionService) StartAuction(ctx context.Context, id, seller string, endsAt time.Time) error {
	ttl := int(time.Until(endsAt).Seconds())
	if ttl <= 0 {
		return ErrAuctionClosed
	}

	// DB sanity check (2 s timeout)
	dbCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	var st string
	err := svc.db.QueryRowContext(dbCtx, `SELECT status FROM auctions WHERE id = $1`, id).Scan(&st)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	switch st {
	case "RUNNING":
		return ErrAlreadyRunning
	case "FINISHED":
		return ErrAuctionFinished
	}

	return svc.rdc.FCall(ctx, "auction_start",
		[]string{
			redisAuctionKeyPrefix + id,      // "auc:<id>"
			redisAuctionTimerKeyPrefix + id, // timer key
		},
		seller,
		time.Now().Unix(),
		endsAt.Unix(),
		ttl,
	).Err()
}

// Stop lets seller cancel early (or system close). We simply delete the key.
func (svc *auctionService) StopAuction(ctx context.Context, auctionID string) error {

	// If DB already shows FINISHED refuse the request
	var st string
	err := svc.db.QueryRowContext(ctx, `SELECT status FROM auctions WHERE id = $1`, auctionID).Scan(&st)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if st == "FINISHED" {
		return ErrAuctionFinished
	}

	// Otherwise perform the usual finalisation path (idempotent).
	if err := svc.Finalize(ctx, auctionID); err != nil {
		return err
	}

	// ensure TTL key removed
	_ = svc.rdc.Del(ctx, redisAuctionTimerKeyPrefix+auctionID).Err()
	return nil
}

// Bid executes Lua function that performs optimistic check & Pub/Sub.
func (svc *auctionService) PlaceBid(ctx context.Context, auctionID, bidderID string, amount float64) error {

	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()

	now := time.Now().Unix()
	res := svc.rdc.FCall(ctx, "auction_place_bid",
		[]string{
			redisAuctionKeyPrefix + auctionID,
			redisAuctionTimerKeyPrefix + auctionID,
		},
		bidderID,
		amount,
		now,
		svc.minIncrement,
	)
	if err := res.Err(); err != nil {
		if strings.Contains(err.Error(), "auction_closed") {
			return ErrAuctionClosed
		}
		if strings.Contains(err.Error(), "bid_equal") {
			return ErrBidEqual
		}
		if strings.Contains(err.Error(), "bid_below_current") {
			return ErrBidBelowCurrent
		}
		if strings.Contains(err.Error(), "bid_below_increment") {
			return ErrBidBelowIncrement
		}
		return err
	}
	return nil
}

// called by key‑expiry watcher
func (svc *auctionService) Finalize(ctx context.Context, id string) error {
	// distributed, 5 s lock – avoids duplicate finalisations
	lockKey := "auc_lock:" + id
	ok, _ := svc.rdc.SetNX(ctx, lockKey, 1, 5*time.Second).Result()
	if !ok {
		return nil // another goroutine is already finalising the same auction
	}
	defer svc.rdc.Del(ctx, lockKey) // snapshot hash -> result (makes DB write idempotent)

	key := redisAuctionKeyPrefix + id
	data, err := svc.rdc.HGetAll(ctx, key).Result()
	if err != nil || len(data) == 0 {
		return err
	}

	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Up‑sert so that we also persist auctions that finished before the 10 s
	// high‑bid synchroniser had a chance to create their row.
	const upsertQ = `
	  INSERT INTO auctions (id, seller_id, item, starts_at, ends_at,
	                        status,  high_bid, high_bidder)
	       VALUES           ($1, $2,        '', to_timestamp($3), to_timestamp($4),
	                        'FINISHED', $5,       $6)
	  ON CONFLICT (id) DO UPDATE
	        SET status     = 'FINISHED',
	            high_bid   = EXCLUDED.high_bid,
	            high_bidder= EXCLUDED.high_bidder`

	_, err = tx.ExecContext(ctx, upsertQ,
		id,
		data["sid"],
		data["sa"],
		data["ea"],
		data["hb"],
		data["hbid"],
	)
	if err != nil {
		return err
	}

	if data["hb"] != "0" && data["hbid"] != "" {
		const insBid = `
		  INSERT INTO bids (auction_id, bidder_id, amount)
		      VALUES ($1, $2, $3)
		  ON CONFLICT DO NOTHING`
		if _, err = tx.ExecContext(ctx, insBid, id, data["hbid"], data["hb"]); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	// broadcast and clean redis
	return svc.rdc.FCall(ctx, "auction_stop",
		[]string{
			key,
			redisAuctionTimerKeyPrefix + id,
		}).Err()
}
func (svc *auctionService) GetAuction(ctx context.Context, id string) (*AuctionDTO, error) {
	// 1. Fast‑path ‑ if it is RUNNING, serve directly from Redis
	snap, _ := svc.rdc.HGetAll(ctx, redisAuctionKeyPrefix+id).Result()
	if st, ok := snap["st"]; ok && st == "RUNNING" {
		return &AuctionDTO{
			ID:         id,
			SellerID:   snap["sid"],
			StartsAt:   ts(snap["sa"]),
			EndsAt:     ts(snap["ea"]),
			Status:     st,
			HighBid:    atof(snap["hb"]),
			HighBidder: snap["hbid"],
		}, nil
	}

	// 2. Otherwise go to Postgres
	const q = `SELECT id, seller_id, starts_at, ends_at,
                      status, coalesce(high_bid,0), coalesce(high_bidder,'')
                 FROM auctions WHERE id = $1`
	row := svc.db.QueryRowContext(ctx, q, id)
	dto := &AuctionDTO{}
	if err := row.Scan(&dto.ID, &dto.SellerID,
		&dto.StartsAt, &dto.EndsAt, &dto.Status,
		&dto.HighBid, &dto.HighBidder); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("auction %s not found", id)
		}
		return nil, err
	}
	return dto, nil
}

func (svc *auctionService) ListAuctions(ctx context.Context, st string,
	limit, offset int) ([]AuctionDTO, error) {

	if limit == 0 {
		limit = 10
	}
	var (
		rows *sql.Rows
		err  error
	)
	base := `SELECT id, seller_id, starts_at, ends_at,
                    status, coalesce(high_bid,0), coalesce(high_bidder,'')
               FROM auctions`
	switch st {
	case "RUNNING", "FINISHED":
		base += " WHERE status = $1"
		rows, err = svc.db.QueryContext(ctx, base+" ORDER BY ends_at DESC LIMIT $2 OFFSET $3",
			st, limit, offset)
	default:
		rows, err = svc.db.QueryContext(ctx, base+" ORDER BY ends_at DESC LIMIT $1 OFFSET $2",
			limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AuctionDTO, 0, limit)
	for rows.Next() {
		var a AuctionDTO
		if err := rows.Scan(&a.ID, &a.SellerID, &a.StartsAt,
			&a.EndsAt, &a.Status, &a.HighBid, &a.HighBidder); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

// helpers
func ts(s string) time.Time {
	i, _ := strconv.ParseInt(s, 10, 64)
	return time.Unix(i, 0).UTC()
}
func atof(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// DeleteAuction removes all traces of an auction provided it is not RUNNING.
func (svc *auctionService) DeleteAuction(ctx context.Context, id string) error {
	// ── 1. Fast check in Redis (if hash exists) ───────────────────────
	st, _ := svc.rdc.HGet(ctx, redisAuctionKeyPrefix+id, "st").Result()
	if st == "RUNNING" {
		return ErrAuctionRunning
	}

	// ── 2. Check status in Postgres ───────────────────────────────────
	var dbStatus string
	err := svc.db.QueryRowContext(ctx,
		`SELECT status FROM auctions WHERE id = $1`, id).Scan(&dbStatus)

	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err // unexpected DB error
	}
	if dbStatus == "RUNNING" { // row exists and is RUNNING → forbid
		return ErrAuctionRunning
	}
	if errors.Is(err, sql.ErrNoRows) && st == "" { // nothing to delete
		return fmt.Errorf("auction %s not found", id)
	}

	// ── 3. Postgres: delete bids first, then the auction  ─────────────
	tx, err := svc.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err = tx.ExecContext(ctx,
		`DELETE FROM bids WHERE auction_id = $1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx,
		`DELETE FROM auctions WHERE id = $1`, id); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	// ── 4. Redis: purge keys & sets  (idempotent ops) ─────────────────
	_ = svc.rdc.Del(ctx,
		redisAuctionKeyPrefix+id,
		redisAuctionTimerKeyPrefix+id).Err()
	_ = svc.rdc.SRem(ctx, "aucs:active", redisAuctionKeyPrefix+id).Err()
	_ = svc.rdc.SRem(ctx, "aucs:ended", redisAuctionKeyPrefix+id).Err()

	return nil
}
