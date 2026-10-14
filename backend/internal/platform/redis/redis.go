// Package redis tạo client Redis.
package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

func Connect(ctx context.Context, addr string) (*goredis.Client, error) {
	rdb := goredis.NewClient(&goredis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return rdb, nil
}
