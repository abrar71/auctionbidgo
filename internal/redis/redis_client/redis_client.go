package redis_client

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// returns a new Redis client
func NewRedisClient(host string, port int) (*redis.Client, error) {

	maxPool := runtime.NumCPU() * 8
	if maxPool > 512 {
		maxPool = 512
	}

	rc := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", host, port),
		PoolSize: maxPool,
	})

	ctx, cancelFunc := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelFunc()
	_, err := rc.Ping(ctx).Result()
	if err != nil {
		err = errors.New("Redis connection failed: " + err.Error())
		zap.L().Error("redis_connect", zap.Error(err))
		return nil, err
	}
	return rc, nil
}
