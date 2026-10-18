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

	"github.com/proonhit/realtime-price-board/backend/internal/auth"
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
	// HS256 nên dùng secret ít nhất 32 byte (256 bit). Chỉ cảnh báo để không chặn chạy local.
	if len(cfg.JWTSecret) < 32 {
		log.Println("WARNING: JWT_SECRET is unset or shorter than 32 bytes; set a long random value outside local dev")
	}
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
	tokens := auth.Tokens{Secret: []byte(cfg.JWTSecret), TTL: cfg.AccessTTL}
	authSvc := auth.NewService(auth.NewStore(db), tokens, cfg.RefreshTTL)
	hub := realtime.NewHub()

	busErr := make(chan error, 1)
	go func() { busErr <- realtime.NewBus(rdb).Subscribe(ctx, hub.Broadcast) }()

	mux := http.NewServeMux()
	price.NewHandler(prices).Register(mux)
	// Middleware dựng một lần ở đây rồi truyền cho các package cần bảo vệ route.
	requireAuth := auth.RequireAuth(tokens)
	watchlist.NewHandler(watch, requireAuth).Register(mux)
	auth.NewHandler(authSvc, tokens).Register(mux)
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
