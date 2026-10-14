package realtime

import "testing"

func TestBroadcastReachesAllClients(t *testing.T) {
	h := NewHub()
	a, b := newClient(), newClient()
	h.register(a)
	h.register(b)

	h.Broadcast([]byte("hi"))

	for name, c := range map[string]*client{"a": a, "b": b} {
		if got := string(<-c.send); got != "hi" {
			t.Errorf("client %s got %q, want %q", name, got, "hi")
		}
	}
}

func TestBroadcastDropsSlowClient(t *testing.T) {
	h := NewHub()
	slow, fast := newClient(), newClient()
	h.register(slow)
	h.register(fast)

	// slow không đọc gì; fast đọc hết sau mỗi lần broadcast.
	for range sendBuffer + 1 {
		h.Broadcast([]byte("tick"))
		<-fast.send
	}

	if h.Len() != 1 {
		t.Fatalf("Len() = %d, want 1 (slow client dropped)", h.Len())
	}
	n := 0
	for range slow.send { // channel phải được đóng, nếu không vòng lặp sẽ treo
		n++
	}
	if n != sendBuffer {
		t.Errorf("slow client buffered %d messages, want %d", n, sendBuffer)
	}
}

func TestUnregisterIsIdempotent(t *testing.T) {
	h := NewHub()
	c := newClient()
	h.register(c)

	h.unregister(c)
	h.unregister(c) // không được panic vì close channel hai lần

	if h.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", h.Len())
	}
}
