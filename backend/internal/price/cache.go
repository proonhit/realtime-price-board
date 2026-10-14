package price

import (
	"context"
	"encoding/json"

	goredis "github.com/redis/go-redis/v9"
)

const latestKey = "price:latest"

// Cache giữ giá mới nhất của mỗi mã trong một Redis hash.
type Cache struct {
	rdb *goredis.Client
}

func NewCache(rdb *goredis.Client) *Cache { return &Cache{rdb: rdb} }

func (c *Cache) Set(ctx context.Context, t Tick) error {
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return c.rdb.HSet(ctx, latestKey, t.Symbol, b).Err()
}

func (c *Cache) All(ctx context.Context) ([]Tick, error) {
	vals, err := c.rdb.HGetAll(ctx, latestKey).Result()
	if err != nil {
		return nil, err
	}
	out := make([]Tick, 0, len(vals))
	for _, v := range vals {
		var t Tick
		if err := json.Unmarshal([]byte(v), &t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}
