package ws

import (
	"auctionbidgo/internal/services/auction"
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 12 * time.Second
	pingPeriod = 3 * time.Second // must be < pongWait to avoid ping timeout
)

type WsServer struct {
	hub        *Hub
	rdc        *redis.Client
	auctionSvc auction.IAuctionService
	up         websocket.Upgrader
}

func NewWsServer(h *Hub, rdc *redis.Client, auctionSvc auction.IAuctionService) *WsServer {
	return &WsServer{
		hub:        h,
		rdc:        rdc,
		auctionSvc: auctionSvc,
		up: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true }, // allow all CORS for dev
		},
	}
}

func (s *WsServer) Handle(ginCtx *gin.Context) {
	auctionID := ginCtx.Query("auction_id")
	if auctionID == "" {
		ginCtx.JSON(http.StatusBadRequest, gin.H{"error": "auction_id missing"})
		return
	}

	conn, err := s.up.Upgrade(ginCtx.Writer, ginCtx.Request, nil)
	if err != nil {
		zap.L().Warn("ws-upgrade", zap.Error(err))
		return
	}
	wsConn := &clientConn{rawConn: conn}

	s.hub.Join(auctionID, wsConn)

	// Send snapshot synchronously so it is guaranteed to arrive first
	if err := s.pushInitialSnapshot(ginCtx.Request.Context(), auctionID, wsConn); err != nil {
		zap.L().Warn("ws.snapshot", zap.Error(err))
	}

	// Reader (leaves on error/timeout)
	go s.reader(auctionID, wsConn)
	// Writer ping keep‑alive
	go s.pinger(wsConn)
}

// pushInitialSnapshot now falls back to Postgres when the hash no longer exists.
func (s *WsServer) pushInitialSnapshot(ctx context.Context, id string, conn *clientConn) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	// Attempt fast‑path from Redis
	snap, err := s.rdc.HGetAll(ctx, "auc:"+id).Result()
	if err == nil && len(snap) != 0 {
		return conn.writeJSON(gin.H{"event": "snapshot", "data": snap})
	}

	// Fallback: read from database
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
	defer s.hub.Leave(auctionID, conn)

	conn.rawConn.SetReadLimit(512)

	_ = conn.rawConn.SetReadDeadline(time.Now().Add(pongWait))

	conn.rawConn.SetPongHandler(func(string) error {
		_ = conn.rawConn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := conn.rawConn.ReadMessage(); err != nil {
			return
		}
	}
}

func (s *WsServer) pinger(conn *clientConn) {
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for range ticker.C {
		// NOW goes through the same mutex as normal messages
		if err := conn.write(websocket.PingMessage, nil); err != nil {
			conn.rawConn.Close()
			return
		}
	}
}
