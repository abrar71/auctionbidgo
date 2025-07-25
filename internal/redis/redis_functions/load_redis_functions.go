package redis_functions

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

//go:embed *.lua
var fs embed.FS

// LoadAll finds every embedded Lua file and loads/replaces it in Redis.
func LoadAll(ctx context.Context, rdb *redis.Client) error {
	files, err := fs.ReadDir(".")
	if err != nil {
		return fmt.Errorf("read embed dir: %w", err)
	}
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".lua") {
			continue
		}

		code, err := fs.ReadFile(f.Name())
		if err != nil {
			return err
		}
		if err := rdb.FunctionLoadReplace(ctx, string(code)).Err(); err != nil {
			return fmt.Errorf("load lua %s: %w", f.Name(), err)
		}
		zap.L().Info("lua function loaded", zap.String("file", f.Name()))
	}
	return nil
}
