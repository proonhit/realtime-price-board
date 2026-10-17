package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

// fakeSnapshot thay cho price.Service: trả về danh sách tick cố định.
type fakeSnapshot struct{ ticks []price.Tick }

func (f fakeSnapshot) Latest(context.Context) ([]price.Tick, error) { return f.ticks, nil }

// Thời gian chờ tối đa cho mọi thao tác trong test, để test không bao giờ treo mãi.
const testTimeout = 2 * time.Second

// startServer chạy một HTTP server thật trên cổng ngẫu nhiên của localhost
// và kết nối một WebSocket client tới /ws.
func startServer(t *testing.T, hub *Hub, snap snapshotter) *websocket.Conn {
	t.Helper()
	mux := http.NewServeMux()
	NewHandler(hub, snap).Register(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// srv.URL có dạng "http://127.0.0.1:54321"; WebSocket cần scheme ws://.
	url := "ws://" + strings.TrimPrefix(srv.URL, "http://") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	// Cleanup chạy theo thứ tự ngược (LIFO): đóng client trước, rồi mới tắt server.
	t.Cleanup(func() { conn.Close() })
	return conn
}

// readTick đọc một message và decode thành Tick. Read deadline đảm bảo
// nếu server không gửi gì thì ReadMessage trả lỗi timeout thay vì chờ vô hạn.
func readTick(t *testing.T, conn *websocket.Conn) price.Tick {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(testTimeout))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read message: %v", err)
	}
	var tick price.Tick
	if err := json.Unmarshal(msg, &tick); err != nil {
		t.Fatalf("decode %s: %v", msg, err)
	}
	return tick
}

// waitFor kiểm tra cond lặp lại mỗi 5ms cho tới khi đúng hoặc hết testTimeout.
// Dùng khi kết quả xảy ra ở goroutine khác (phía server) nên không biết chính xác lúc nào.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHandlerSendsSnapshotOnConnect(t *testing.T) {
	snap := fakeSnapshot{ticks: []price.Tick{
		{Symbol: "FPT", Price: 120, Ts: 1},
		{Symbol: "VNM", Price: 65, Ts: 1},
	}}
	conn := startServer(t, NewHub(), snap)

	// Snapshot được gửi theo đúng thứ tự snapshotter trả về.
	for _, want := range snap.ticks {
		if got := readTick(t, conn); got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	}
}

func TestHandlerForwardsBroadcast(t *testing.T) {
	hub := NewHub()
	conn := startServer(t, hub, fakeSnapshot{}) // snapshot rỗng

	// Dial xong chưa chắc server đã register client vào hub (việc đó chạy ở
	// goroutine của server). Broadcast trước lúc đó thì message sẽ bị mất.
	waitFor(t, "client registered", func() bool { return hub.Len() == 1 })

	want := price.Tick{Symbol: "HPG", Price: 27.5, Ts: 42}
	msg, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	hub.Broadcast(msg)

	if got := readTick(t, conn); got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestHandlerUnregistersOnClientClose(t *testing.T) {
	hub := NewHub()
	conn := startServer(t, hub, fakeSnapshot{})
	waitFor(t, "client registered", func() bool { return hub.Len() == 1 })

	// Client đóng kết nối → readPump phía server gặp lỗi đọc → hub.unregister.
	conn.Close()

	waitFor(t, "client unregistered", func() bool { return hub.Len() == 0 })
}
