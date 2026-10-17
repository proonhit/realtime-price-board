package watchlist

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/proonhit/realtime-price-board/backend/internal/auth"
)

// Dùng middleware thật auth.RequireAuth với secret dành riêng cho test, để test
// cả đường đi "không có token → 401" giống hệt lúc chạy thật.
var testTokens = auth.Tokens{Secret: []byte("test-secret-at-least-32-bytes-long!!"), TTL: time.Minute}

func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(NewService(newMemStore()), auth.RequireAuth(testTokens)).Register(mux)
	return mux
}

// bearer ký access token cho userID và trả về giá trị header Authorization.
func bearer(t *testing.T, userID string) string {
	t.Helper()
	tok, err := testTokens.IssueAccess(userID)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

func do(mux http.Handler, method, path, body, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []string {
	t.Helper()
	var syms []string
	if err := json.NewDecoder(rec.Body).Decode(&syms); err != nil {
		t.Fatal(err)
	}
	return syms
}

func TestHandlerRequiresAuth(t *testing.T) {
	mux := newTestMux()
	tests := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/watchlist", ""},
		{http.MethodPost, "/api/watchlist", `{"symbol":"FPT"}`},
		{http.MethodDelete, "/api/watchlist/FPT", ""},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			if rec := do(mux, tt.method, tt.path, tt.body, ""); rec.Code != http.StatusUnauthorized {
				t.Fatalf("without token status = %d, want 401", rec.Code)
			}
			if rec := do(mux, tt.method, tt.path, tt.body, "Bearer garbage"); rec.Code != http.StatusUnauthorized {
				t.Fatalf("bad token status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestHandlerStatusCodes(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		wantStatus               int
	}{
		{"list empty", http.MethodGet, "/api/watchlist", "", http.StatusOK},
		{"add", http.MethodPost, "/api/watchlist", `{"symbol":"fpt"}`, http.StatusOK},
		{"add invalid JSON", http.MethodPost, "/api/watchlist", `{`, http.StatusBadRequest},
		{"add unknown symbol", http.MethodPost, "/api/watchlist", `{"symbol":"AAPL"}`, http.StatusBadRequest},
		{"remove missing", http.MethodDelete, "/api/watchlist/FPT", "", http.StatusNotFound},
		{"remove unknown symbol", http.MethodDelete, "/api/watchlist/AAPL", "", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestMux(), tt.method, tt.path, tt.body, bearer(t, "alice"))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestHandlerIsolatesUsers(t *testing.T) {
	mux := newTestMux()
	alice, bob := bearer(t, "alice"), bearer(t, "bob")

	if rec := do(mux, http.MethodPost, "/api/watchlist", `{"symbol":"FPT"}`, alice); rec.Code != http.StatusOK {
		t.Fatalf("alice add status = %d", rec.Code)
	}
	if rec := do(mux, http.MethodPost, "/api/watchlist", `{"symbol":"VNM"}`, bob); rec.Code != http.StatusOK {
		t.Fatalf("bob add status = %d", rec.Code)
	}

	if got := decodeList(t, do(mux, http.MethodGet, "/api/watchlist", "", alice)); !slices.Equal(got, []string{"FPT"}) {
		t.Fatalf("alice sees %v, want [FPT]", got)
	}
	if got := decodeList(t, do(mux, http.MethodGet, "/api/watchlist", "", bob)); !slices.Equal(got, []string{"VNM"}) {
		t.Fatalf("bob sees %v, want [VNM]", got)
	}

	// bob không xoá được mã của alice: với bob, FPT "không có trong danh sách".
	if rec := do(mux, http.MethodDelete, "/api/watchlist/FPT", "", bob); rec.Code != http.StatusNotFound {
		t.Fatalf("bob delete alice's FPT status = %d, want 404", rec.Code)
	}
	if rec := do(mux, http.MethodDelete, "/api/watchlist/FPT", "", alice); rec.Code != http.StatusNoContent {
		t.Fatalf("alice delete FPT status = %d, want 204", rec.Code)
	}
}

// Nếu ai đó truyền middleware không gắn user id (ví dụ quên RequireAuth),
// handler phải trả 401 chứ không được chạy tiếp với user id rỗng.
func TestHandlerWithoutUserInContext(t *testing.T) {
	passThrough := func(next http.Handler) http.Handler { return next }
	mux := http.NewServeMux()
	NewHandler(NewService(newMemStore()), passThrough).Register(mux)

	if rec := do(mux, http.MethodGet, "/api/watchlist", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
