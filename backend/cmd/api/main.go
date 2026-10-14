// Command api phục vụ REST API và WebSocket. Có thể chạy nhiều instance song song:
// tất cả cùng nhận giá từ Redis Pub/Sub do producer phát ra.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/config"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/postgres"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/redis"
	"github.com/proonhit/realtime-price-board/backend/internal/price"
	"github.com/proonhit/realtime-price-board/backend/internal/realtime"
	"github.com/proonhit/realtime-price-board/backend/internal/watchlist"
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

	prices := price.NewService(price.NewStore(db), price.NewCache(rdb))
	watch := watchlist.NewService(watchlist.NewStore(db))
	hub := realtime.NewHub()

	busErr := make(chan error, 1)
	go func() { busErr <- realtime.NewBus(rdb).Subscribe(ctx, hub.Broadcast) }()

	mux := http.NewServeMux()
	price.NewHandler(prices).Register(mux)
	watchlist.NewHandler(watch).Register(mux)
	realtime.NewHandler(hub, prices).Register(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	srvErr := make(chan error, 1)
	go func() {
		log.Printf("api listening on %s", cfg.HTTPAddr)
		srvErr <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Println("shutting down")
	case err := <-srvErr:
		return err
	case err := <-busErr:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
