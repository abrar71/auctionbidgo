package ws

import (
	"auctionbidgo/internal/services/auction"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
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
	rdc        *redis.Client
	auctionSvc auction.IAuctionService
}

func NewWsServer(h *Hub, rdc *redis.Client, auctionSvc auction.IAuctionService) *WsServer {
	return &WsServer{
		hub:        h,
		rdc:        rdc,
		subMgr:     newSubscriptionManager(rdc, h),
		auctionSvc: auctionSvc,
	}
}

func (s *WsServer) Handle(ginCtx *gin.Context) {
	auctionID := ginCtx.Query("auction_id")
	if auctionID == "" {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": "auction_id missing"})
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

	// ─────────────────────────────────── Client joined ─────────────────────────
	wsConn := &clientConn{rawConn: rawConn}
	s.hub.Join(auctionID, wsConn)
	s.subMgr.Subscribe(auctionID) //may be a no‑operation (i.e. already subscribed)

	// send initial snapshot
	if err := s.pushInitialSnapshot(ginCtx.Request.Context(), auctionID, wsConn); err != nil {
		if !strings.Contains(err.Error(), "not found") {
			zap.L().Warn("ws.snapshot", zap.Error(err))
		}
	}

	go s.reader(auctionID, wsConn)
	go s.pinger(wsConn)
}

// -------------------------------- private helpers ---------------------------

func (s *WsServer) pushInitialSnapshot(ctx context.Context, id string, conn *clientConn) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if snap, _ := s.rdc.HGetAll(ctx, "auc:"+id).Result(); len(snap) != 0 {
		return conn.writeJSON(gin.H{"event": "snapshot", "data": snap})
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
	return conn.writeJSON(gin.H{"event": "snapshot", "data": dbSnap})
}

func (s *WsServer) reader(auctionID string, conn *clientConn) {
	defer func() {
		s.hub.Leave(auctionID, conn)
		s.subMgr.Unsubscribe(auctionID) // last client? → drop Redis SUB
	}()

	ctx := context.Background()
	for {
		if _, _, err := conn.rawConn.Read(ctx); err != nil {
			return // client closed or errored
		}
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
