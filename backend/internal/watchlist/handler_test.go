package watchlist

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// errStore giả lập storage bị lỗi để kiểm tra nhánh 500.
type errStore struct{}

var errDB = errors.New("dial tcp 10.0.0.5:5432: connection refused")

func (errStore) List(context.Context) ([]string, error)       { return nil, errDB }
func (errStore) Add(context.Context, string) error            { return errDB }
func (errStore) Remove(context.Context, string) (bool, error) { return false, errDB }

func TestHandler(t *testing.T) {
	tests := []struct {
		name       string
		seed       []string // dữ liệu có sẵn trong memStore trước khi gọi API
		broken     bool     // true: dùng errStore thay cho memStore
		method     string
		path       string
		body       string
		wantStatus int
		wantBody   string // rỗng nghĩa là response không có body (204)
	}{
		{name: "list", seed: []string{"VNM", "FPT"}, method: http.MethodGet, path: "/api/watchlist",
			wantStatus: http.StatusOK, wantBody: `["FPT","VNM"]`},
		{name: "add normalizes symbol", seed: []string{"FPT"}, method: http.MethodPost, path: "/api/watchlist",
			body: `{"symbol":" vnm "}`, wantStatus: http.StatusOK, wantBody: `["FPT","VNM"]`},
		{name: "add invalid JSON", method: http.MethodPost, path: "/api/watchlist",
			body: `{"symbol":`, wantStatus: http.StatusBadRequest, wantBody: `{"error":"invalid JSON body"}`},
		{name: "add unknown symbol", method: http.MethodPost, path: "/api/watchlist",
			body: `{"symbol":"AAPL"}`, wantStatus: http.StatusBadRequest, wantBody: `{"error":"unknown symbol"}`},
		// Body 2KB vượt giới hạn 1KB của http.MaxBytesReader nên Decode trả lỗi.
		{name: "add body too large", method: http.MethodPost, path: "/api/watchlist",
			body:       `{"symbol":"` + strings.Repeat("A", 2048) + `"}`,
			wantStatus: http.StatusBadRequest, wantBody: `{"error":"invalid JSON body"}`},
		{name: "remove existing", seed: []string{"FPT"}, method: http.MethodDelete, path: "/api/watchlist/fpt",
			wantStatus: http.StatusNoContent},
		{name: "remove not in list", seed: []string{"FPT"}, method: http.MethodDelete, path: "/api/watchlist/VNM",
			wantStatus: http.StatusNotFound, wantBody: `{"error":"symbol is not in watchlist"}`},
		{name: "remove unknown symbol", method: http.MethodDelete, path: "/api/watchlist/AAPL",
			wantStatus: http.StatusNotFound, wantBody: `{"error":"unknown symbol"}`},
		{name: "list store error", broken: true, method: http.MethodGet, path: "/api/watchlist",
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal error"}`},
		{name: "add store error", broken: true, method: http.MethodPost, path: "/api/watchlist",
			body: `{"symbol":"FPT"}`, wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal error"}`},
		{name: "remove store error", broken: true, method: http.MethodDelete, path: "/api/watchlist/FPT",
			wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mỗi case có store riêng: memStore bị sửa khi POST/DELETE, nếu dùng chung
			// thì kết quả case sau sẽ phụ thuộc case trước.
			var s store = &memStore{syms: slices.Clone(tt.seed)}
			if tt.broken {
				s = errStore{}
			}
			mux := http.NewServeMux()
			NewHandler(NewService(s)).Register(mux)

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}

			if tt.wantBody == "" {
				if rec.Body.Len() != 0 {
					t.Errorf("body = %q, want empty", rec.Body)
				}
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var got, want any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("response is not valid JSON: %v (body: %s)", err, rec.Body)
			}
			if err := json.Unmarshal([]byte(tt.wantBody), &want); err != nil {
				t.Fatalf("bad wantBody in test: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("body = %s, want %s", rec.Body, tt.wantBody)
			}
		})
	}
}
