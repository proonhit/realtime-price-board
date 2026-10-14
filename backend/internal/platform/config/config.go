// Package config đọc cấu hình từ biến môi trường, có giá trị mặc định để chạy local.
package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr     string
	DatabaseURL  string
	RedisAddr    string
	TickInterval time.Duration
}

func Load() Config {
	return Config{
		HTTPAddr:     env("HTTP_ADDR", ":8080"),
		DatabaseURL:  env("DATABASE_URL", "postgres://app:app@localhost:5432/priceboard"),
		RedisAddr:    env("REDIS_ADDR", "localhost:6379"),
		TickInterval: duration("TICK_INTERVAL", 500*time.Millisecond),
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func duration(key string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(key)); err == nil && d > 0 {
		return d
	}
	return def
}
