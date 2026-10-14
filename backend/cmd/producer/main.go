// Command producer sinh giá giả lập, ghi vào PostgreSQL (lịch sử) và Redis
// (giá mới nhất + Pub/Sub). Chỉ nên chạy một instance.
package main

import (
	"context"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/config"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/postgres"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/redis"
	"github.com/proonhit/realtime-price-board/backend/internal/price"
	"github.com/proonhit/realtime-price-board/backend/internal/realtime"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	rdb, err := redis.Connect(ctx, cfg.RedisAddr)
	if err != nil {
		return err
	}
	defer rdb.Close()

	store := price.NewStore(db)
	cache := price.NewCache(rdb)
	bus := realtime.NewBus(rdb)
	seed := uint64(time.Now().UnixNano())
	gen := price.NewGenerator(price.Universe, rand.New(rand.NewPCG(seed, seed>>1)))

	ticker := time.NewTicker(cfg.TickInterval)
	defer ticker.Stop()
	log.Printf("producer started, %d symbols every %s", len(price.Universe), cfg.TickInterval)

	for {
		select {
		case <-ctx.Done():
			log.Println("shutting down")
			return nil
		case now := <-ticker.C:
			for _, t := range gen.Next(now) {
				// Đẩy realtime trước để giảm độ trễ, ghi lịch sử sau.
				if err := cache.Set(ctx, t); err != nil {
					log.Printf("cache %s: %v", t.Symbol, err)
				}
				if err := bus.Publish(ctx, t); err != nil {
					log.Printf("publish %s: %v", t.Symbol, err)
				}
				if err := store.Insert(ctx, t); err != nil {
					log.Printf("insert %s: %v", t.Symbol, err)
				}
			}
		}
	}
}
