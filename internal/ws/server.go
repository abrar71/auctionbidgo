package ws

import (
	"auctionbidgo/internal/services/auction"
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 12 * time.Second
	pingPeriod = 3 * time.Second // must be < pongWait
)

type WsServer struct {
	hub        *Hub
	subMgr     *subscriptionManager
	router     *Router
	rdc        *redis.Client
	auctionSvc auction.IAuctionService
}

func NewWsServer(h *Hub, rdc *redis.Client, auctionSvc auction.IAuctionService) *WsServer {
	router := NewRouter()
	srv := &WsServer{
		hub:        h,
		subMgr:     newSubscriptionManager(rdc, h),
		router:     router,
		rdc:        rdc,
		auctionSvc: auctionSvc,
	}
	srv.registerHandlers() // ← all WS endpoints configured here
	return srv
}

// ---------------------------------------------------------------------------
//  Public: Gin entry‑point
// ---------------------------------------------------------------------------

func (s *WsServer) Handle(ginCtx *gin.Context) {
	auctionID := ginCtx.Query("auction_id")
	userID := ginCtx.Query("user_id")
	if auctionID == "" || userID == "" {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": "auction_id and user_id are required"})
		return
	}

	rawConn, err := websocket.Accept(
		ginCtx.Writer, ginCtx.Request,
		&websocket.AcceptOptions{InsecureSkipVerify: true}, // dev‑only
	)
	if err != nil {
		zap.L().Warn("ws.accept", zap.Error(err))
		return
	}
	rawConn.SetReadLimit(512)

	// ─────────────────── Client joined ────────────────────────
	wsConn := &clientConn{rawConn: rawConn}
	s.hub.Join(auctionID, wsConn)
	s.subMgr.Subscribe(auctionID) // may be a no‑op (already subscribed)

	// Initial snapshot.
	if err := s.pushInitialSnapshot(ginCtx.Request.Context(), auctionID, wsConn); err != nil &&
		!strings.Contains(err.Error(), "not found") {
		zap.L().Warn("ws.snapshot", zap.Error(err))
	}

	go s.reader(auctionID, userID, wsConn)
	go s.pinger(wsConn)
}

// ---------------------------------------------------------------------------
//  Private helpers
// ---------------------------------------------------------------------------

func (s *WsServer) registerHandlers() {
	// 🔹 auctions/bid ---------------------------------------------------------
	Register(
		s.router,
		"auctions/bid",
		func(ctx context.Context, cc *ConnContext, req BidRequest) (AckBody, error) {
			if req.Amount <= 0 {
				return AckBody{}, errors.New("invalid_amount")
			}
			err := s.auctionSvc.PlaceBid(ctx, cc.AuctionID, cc.UserID, req.Amount)
			return AckBody{}, err
		},
	)
}

func (s *WsServer) pushInitialSnapshot(ctx context.Context, id string, conn *clientConn) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if snap, _ := s.rdc.HGetAll(ctx, "auc:"+id).Result(); len(snap) != 0 {
		return conn.writeJSON(gin.H{
			"event": "auctions/snapshot",
			"body":  snap,
		})
	}

	dto, err := s.auctionSvc.GetAuction(ctx, id)
	if err != nil {
		return err
	}
	dbSnap := gin.H{
		"sid":  dto.SellerID,
		"sa":   dto.StartsAt.Unix(),
		"ea":   dto.EndsAt.Unix(),
		"st":   dto.Status,
		"hb":   strconv.FormatFloat(dto.HighBid, 'f', -1, 64),
		"hbid": dto.HighBidder,
	}
	return conn.writeJSON(gin.H{
		"event": "auctions/snapshot",
		"body":  dbSnap,
	})
}

func (s *WsServer) reader(auctionID, userID string, conn *clientConn) {
	defer func() {
		s.hub.Leave(auctionID, conn)
		s.subMgr.Unsubscribe(auctionID)
	}()

	cc := &ConnContext{AuctionID: auctionID, UserID: userID, Server: s}

	for {
		var env Envelope
		if err := wsjson.Read(context.Background(), conn.rawConn, &env); err != nil {
			return // client closed or errored
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1900*time.Millisecond)
		res, err := s.router.dispatch(ctx, cc, env)
		cancel()

		// ---- error -> {"event":"error", "body":{...}} ---------------
		if err != nil {
			_ = conn.writeJSON(map[string]any{
				"event": "error",
				"body":  ErrorBody{Error: err.Error()},
			})
			continue
		}

		// ---- success -> {"event":"<evt>-ack", "body":{...}} --------
		reply := map[string]any{"event": env.Event + "-ack"}
		if res != nil {
			reply["body"] = res
		}
		_ = conn.writeJSON(reply)
	}
}

func (s *WsServer) pinger(conn *clientConn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for range ticker.C {
		ctx, cancel := context.WithTimeout(context.Background(), writeWait)
		err := conn.rawConn.Ping(ctx)
		cancel()
		if err != nil {
			_ = conn.rawConn.Close(websocket.StatusNormalClosure, "ping timeout")
			return
		}
	}
}
