package realtime

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = pongWait * 9 / 10
)

type snapshotter interface {
	Latest(ctx context.Context) ([]price.Tick, error)
}

type Handler struct {
	hub      *Hub
	snap     snapshotter
	upgrader websocket.Upgrader
}

func NewHandler(hub *Hub, snap snapshotter) *Handler {
	return &Handler{
		hub:  hub,
		snap: snap,
		// Trình duyệt đi qua nginx / Vite proxy nên Origin có thể khác Host.
		// Đây là dữ liệu public, chỉ đọc; nếu có auth thì cần kiểm tra Origin chặt hơn.
		upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /ws", h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return // Upgrade đã tự trả lỗi HTTP
	}

	c := newClient()
	// Snapshot được xếp vào hàng đợi trước khi register, nên luôn tới client
	// trước các tick mới hơn từ Broadcast.
	ticks, err := h.snap.Latest(r.Context())
	if err != nil {
		log.Printf("ws snapshot: %v", err)
	}
	for _, t := range ticks {
		msg, err := json.Marshal(t)
		if err != nil {
			continue
		}
		select {
		case c.send <- msg:
		default: // nhiều mã hơn sendBuffer: phần còn lại sẽ tới qua tick kế tiếp
		}
	}
	h.hub.register(c)

	go writePump(conn, c)
	readPump(conn)
	h.hub.unregister(c)
}

// readPump chỉ để phát hiện client ngắt kết nối và nhận pong; client không gửi dữ liệu.
func readPump(conn *websocket.Conn) {
	conn.SetReadLimit(512)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			return
		}
	}
}

// writePump là goroutine duy nhất ghi vào conn (gorilla/websocket không cho ghi đồng thời).
func writePump(conn *websocket.Conn, c *client) {
	ping := time.NewTicker(pingPeriod)
	defer func() {
		ping.Stop()
		conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok { // hub đã gỡ client
				conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ping.C:
			conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
