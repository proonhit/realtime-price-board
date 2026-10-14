// Package realtime đẩy giá tới trình duyệt: Redis Pub/Sub → Hub → WebSocket.
package realtime

import "sync"

const sendBuffer = 64

// client đại diện cho một kết nối WebSocket. Chỉ goroutine writePump được ghi vào conn,
// mọi nơi khác gửi dữ liệu qua channel send.
type client struct {
	send chan []byte
}

func newClient() *client { return &client{send: make(chan []byte, sendBuffer)} }

// Hub giữ danh sách client và phát message tới tất cả.
type Hub struct {
	mu      sync.Mutex
	clients map[*client]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[*client]struct{}{}} }

func (h *Hub) register(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

// unregister gỡ client và đóng channel send. Gọi nhiều lần vẫn an toàn.
func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

// Broadcast gửi msg tới mọi client mà không bao giờ bị chặn: client nào để hàng đợi
// đầy (mạng chậm, tab bị treo) sẽ bị ngắt thay vì làm chậm những client còn lại.
func (h *Hub) Broadcast(msg []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			delete(h.clients, c)
			close(c.send)
		}
	}
}

func (h *Hub) Len() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
