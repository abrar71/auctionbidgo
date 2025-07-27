package main

import (
	"auctionbidgo/internal/config"
	"auctionbidgo/internal/database/db_client"
	"auctionbidgo/internal/http/http_server"
	"auctionbidgo/internal/redis/redis_client"
	"auctionbidgo/internal/redis/redis_functions"
	"auctionbidgo/internal/redis/watcher/auctionwatcher"
	"auctionbidgo/internal/services/auction"
	"auctionbidgo/internal/syncbid"
	"auctionbidgo/internal/syncdb"
	"auctionbidgo/internal/ws"
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	Log, _ = zap.NewDevelopment()
)

func main() {
	defer Log.Sync()
	zap.ReplaceGlobals(Log)

	var err error
	var cfg *config.Config
	var redisClient *redis.Client
	var auctionService auction.IAuctionService

	// 1. Load configuration
	cfg, err = config.LoadConfig()
	if err != nil {
		Log.Fatal("Failed to load configuration", zap.Error(err))
	}
	Log.Debug("Configuration loaded successfully", zap.Any("config", cfg))

	// 2. Context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGINT, syscall.SIGTERM,
	)
	defer stop()

	// 3. Redis
	redisClient, err = redis_client.NewRedisClient(cfg.RedisAuctionsHost, int(cfg.RedisAuctionsPort))
	if err != nil {
		Log.Fatal("Failed to create Redis client", zap.Error(err))
	}
	defer redisClient.Close()
	Log.Debug("Redis client created successfully")

	// Load the Redis Functions lua
	if err := redis_functions.LoadAll(ctx, redisClient); err != nil {
		Log.Fatal("load-redis-funcs", zap.Error(err))
	}

	// 3. Postgres db client
	pgDb, err := db_client.Open(cfg.PostgresHost, cfg.PostgresPort, cfg.PostgresUser, cfg.PostgresPassword, cfg.PostgresDb)
	if err != nil {
		Log.Fatal("pg-open", zap.Error(err))
	}
	defer pgDb.Close()

	// 4. Initialize the services such as auctions, etc.
	auctionService = auction.NewAuctionService(redisClient, pgDb, cfg.BidMinIncrement)

	// 5. Background: key‑expiry watcher ➜ finalise in DB
	go auctionwatcher.Run(ctx, redisClient, auctionService)

	// 6. Background: 10 s high‑bid synchroniser
	syncdb.Run(ctx, redisClient, pgDb)
	syncbid.Run(ctx, redisClient, pgDb)

	// 7. WebSockets hub + Redis fan‑out
	hub := ws.NewHub()

	// 8. Initialize the WS server
	wsSrv := ws.NewWsServer(hub, redisClient, auctionService)

	// 9. HTTP + WS server
	httpServer := http_server.NewHttpServer(ctx, cfg.HttpServerPort, wsSrv, auctionService) // Pass the auctionsService when implemented
	if err := httpServer.Start(); err != nil {
		Log.Fatal("Failed to start HTTP server", zap.Error(err))
	}

}
