package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"math/rand"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type Tick struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
	Ts     int64   `json:"ts"`
}

const channel = "ticks"
const latestKey = "price:latest"

// ---------- WebSocket Hub ----------
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
}

func (h *Hub) add(c *websocket.Conn)    { h.mu.Lock(); h.clients[c] = true; h.mu.Unlock() }
func (h *Hub) remove(c *websocket.Conn) { h.mu.Lock(); delete(h.clients, c); h.mu.Unlock(); c.Close() }

func (h *Hub) broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if err := c.WriteMessage(websocket.TextMessage, msg); err != nil {
			c.Close()
			delete(h.clients, c)
		}
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx := context.Background()

	rdb := redis.NewClient(&redis.Options{Addr: env("REDIS_ADDR", "localhost:6379")})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("redis: ", err)
	}

	db, err := pgxpool.New(ctx, env("DATABASE_URL", "postgres://app:app@localhost:5432/priceboard"))
	if err != nil {
		log.Fatal("postgres: ", err)
	}
	defer db.Close()

	hub := &Hub{clients: map[*websocket.Conn]bool{}}

	go simulate(ctx, rdb, db)   // sinh giá giả lập
	go subscribe(ctx, rdb, hub) // nhận từ Redis → đẩy xuống client

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// gửi snapshot giá mới nhất ngay khi client vừa kết nối
		snap, _ := rdb.HGetAll(ctx, latestKey).Result()
		for _, v := range snap {
			c.WriteMessage(websocket.TextMessage, []byte(v))
		}
		hub.add(c)
		// vòng đọc để phát hiện client ngắt kết nối
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				hub.remove(c)
				return
			}
		}
	})

	http.HandleFunc("/api/history", func(w http.ResponseWriter, r *http.Request) {
		sym := r.URL.Query().Get("symbol")
		rows, err := db.Query(r.Context(),
			`SELECT symbol, price::float8, ts FROM ticks WHERE symbol=$1 ORDER BY ts DESC LIMIT 100`, sym)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		out := []Tick{}
		for rows.Next() {
			var t Tick
			var ts time.Time
			rows.Scan(&t.Symbol, &t.Price, &ts)
			t.Ts = ts.UnixMilli()
			out = append(out, t)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	http.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	log.Println("API listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func simulate(ctx context.Context, rdb *redis.Client, db *pgxpool.Pool) {
	prices := map[string]float64{"VNM": 65, "FPT": 120, "HPG": 27, "VIC": 42, "MWG": 60}
	ticker := time.NewTicker(500 * time.Millisecond)
	for range ticker.C {
		for sym, p := range prices {
			p = math.Round(p*(1+(rand.Float64()-0.5)*0.004)*100) / 100 // dao động ±0.2%
			prices[sym] = p
			t := Tick{Symbol: sym, Price: p, Ts: time.Now().UnixMilli()}
			b, _ := json.Marshal(t)
			rdb.Publish(ctx, channel, b)
			rdb.HSet(ctx, latestKey, sym, b)
			db.Exec(ctx, `INSERT INTO ticks(symbol, price, ts) VALUES ($1,$2,$3)`,
				sym, p, time.UnixMilli(t.Ts))
		}
	}
}

func subscribe(ctx context.Context, rdb *redis.Client, hub *Hub) {
	sub := rdb.Subscribe(ctx, channel)
	for msg := range sub.Channel() {
		hub.broadcast([]byte(msg.Payload))
	}
}