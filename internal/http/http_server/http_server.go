package http_server

import (
	"auctionbidgo/internal/http/auctionhandler"
	"auctionbidgo/internal/services/auction"
	"auctionbidgo/internal/ws"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	ginzap "github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/abrar71/swaggerfilesv2" // swagger embed files
)

type httpServer struct {
	listenPort     uint16
	srv            http.Server
	ln             net.Listener
	auctionService auction.IAuctionService
	wsSrv          *ws.WsServer
	ctx            context.Context
}

func NewHttpServer(ctx context.Context, listenPort uint16, wsSrv *ws.WsServer, auctionService auction.IAuctionService) *httpServer {
	return &httpServer{
		listenPort:     listenPort,
		wsSrv:          wsSrv,
		auctionService: auctionService,
		ctx:            ctx,
	}
}

func (h *httpServer) Start() error {
	var err error
	listenAddr := fmt.Sprintf(":%d", h.listenPort)
	h.ln, err = net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}

	routerEngine := gin.New()

	// Swagger UI and API specs
	routerEngine.StaticFS("/swagger-apis", http.FS(swaggerfilesv2.FS))
	routerEngine.Static("/api-specs", "api_specs")

	// Static files for the web UI
	routerEngine.StaticFile("", "public/index.html")
	routerEngine.StaticFile("/script.js", "public/script.js")

	// routerEngine.Use(ginzap.Ginzap(zap.L(), time.RFC3339, true))
	routerEngine.Use(ginzap.RecoveryWithZap(zap.L(), true))

	// websocket endpoint
	routerEngine.GET("/ws", h.wsSrv.Handle)

	// REST API
	ah := auctionhandler.New(h.auctionService)
	ah.Register(routerEngine)

	h.srv = http.Server{
		Handler: routerEngine,
	}

	return h.srv.Serve(h.ln)
}

// Dispose gracefully shuts the HTTP server down.
// It waits up to 10 s for in‑flight requests to finish.
func (h *httpServer) Dispose() error {
	// Create a context that times‑out after 10 s.
	ctx, cancel := context.WithTimeout(h.ctx, 10*time.Second)
	defer cancel()

	// Ask the server to shut down.
	if err := h.srv.Shutdown(ctx); err != nil {
		zap.L().Error("http_dispose", zap.Error(err))
		return err // e.g. active conns didn’t finish in time
	}

	// If the context’s deadline expired, log it for observability.
	if ctx.Err() == context.DeadlineExceeded {
		zap.L().Error("http_dispose", zap.Error(errors.New("shutdown timed out")))
		log.Println("shutdown timeout (10 s)")
	}

	return nil
}
