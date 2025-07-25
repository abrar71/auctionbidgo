package config

import (
	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

type Config struct {
	RedisAuctionsHost string `env:"REDIS_AUCTIONS_HOST" envDefault:"localhost"`
	RedisAuctionsPort uint16 `env:"REDIS_AUCTIONS_PORT" envDefault:"6379"   validate:"min=1000,max=65535"`

	PostgresHost     string `env:"POSTGRES_HOST"     envDefault:"localhost"`
	PostgresPort     string `env:"POSTGRES_PORT"     envDefault:"5432"`
	PostgresUser     string `env:"POSTGRES_USER"     envDefault:"auction_user"`
	PostgresPassword string `env:"POSTGRES_PASSWORD" envDefault:"auction_password"`
	PostgresDb       string `env:"POSTGRES_DB"       envDefault:"auction_db"`

	BidMinIncrement float64 `env:"BID_MIN_INCREMENT" envDefault:"0" validate:"min=0"`

	HttpServerPort uint16 `env:"HTTP_SERVER_PORT" envDefault:"8085" validate:"min=1000,max=65535"`
}

func LoadConfig() (*Config, error) {
	// Load environment variables from .env file
	err := godotenv.Load(".env")
	if err != nil {
		zap.L().Debug(".env file not found", zap.Error(err))
	}

	cfg := &Config{}
	// Parse config from environment variables
	if err = env.Parse(cfg); err != nil {
		zap.L().Error("config_load_failed", zap.Error(err))
		return nil, err
	}

	// Validate the config
	validate := validator.New()
	err = validate.Struct(cfg)
	if err != nil {
		zap.L().Error("config_validation_failed", zap.Error(err))
		return nil, err
	}
	return cfg, nil
}
