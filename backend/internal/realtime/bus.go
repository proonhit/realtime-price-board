package realtime

import (
	"context"
	"encoding/json"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

const channel = "ticks"

// Bus là kênh Redis Pub/Sub nối producer với (nhiều) instance API.
type Bus struct {
	rdb *goredis.Client
}

func NewBus(rdb *goredis.Client) *Bus { return &Bus{rdb: rdb} }

func (b *Bus) Publish(ctx context.Context, t price.Tick) error {
	msg, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return b.rdb.Publish(ctx, channel, msg).Err()
}

// Subscribe chuyển mọi message nhận được tới fn cho tới khi ctx bị huỷ.
func (b *Bus) Subscribe(ctx context.Context, fn func([]byte)) error {
	sub := b.rdb.Subscribe(ctx, channel)
	defer sub.Close()
	if _, err := sub.Receive(ctx); err != nil {
		return fmt.Errorf("subscribe %s: %w", channel, err)
	}

	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case msg, ok := <-ch:
			if !ok {
				return fmt.Errorf("subscription %s closed", channel)
			}
			fn([]byte(msg.Payload))
		}
	}
}
