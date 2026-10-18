package price

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// errStore giả lập DB/Redis bị lỗi. Nó thoả mãn cả historyReader lẫn latestReader
// nên dùng được cho mọi endpoint để kiểm tra nhánh 500.
type errStore struct{}

// Lỗi này cố tình chứa chi tiết "nhạy cảm" để test chắc chắn nó KHÔNG lọt ra response.
var errDB = errors.New("dial tcp 10.0.0.5:5432: connection refused")

func (errStore) History(context.Context, string, int) ([]Tick, error) { return nil, errDB }
func (errStore) All(context.Context) ([]Tick, error)                  { return nil, errDB }
func (errStore) Get(context.Context, string) (Tick, error)            { return Tick{}, errDB }

// newTestMux dựng router thật (http.ServeMux) giống main.go, chỉ thay storage bằng fake.
func newTestMux(h historyReader, l latestReader) *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(NewService(h, l)).Register(mux)
	return mux
}

func TestHandler(t *testing.T) {
	// Fake trả dữ liệu cố định. History trả "mới nhất trước" giống Store thật,
	// service sẽ đảo lại thành cũ → mới.
	okMux := newTestMux(
		&fakeHistory{ticks: []Tick{{Symbol: "FPT", Price: 121, Ts: 2}, {Symbol: "FPT", Price: 120, Ts: 1}}},
		fakeLatest{ticks: []Tick{{Symbol: "VCB", Price: 90, Ts: 5}, {Symbol: "FPT", Price: 120, Ts: 5}}},
	)
	brokenMux := newTestMux(errStore{}, errStore{})

	tests := []struct {
		name       string
		mux        *http.ServeMux
		path       string
		wantStatus int
		wantBody   string // JSON mong đợi; so sánh sau khi decode nên không phụ thuộc khoảng trắng
	}{
		{"latest sorted by symbol", okMux, "/api/prices", http.StatusOK,
			`[{"symbol":"FPT","price":120,"ts":5},{"symbol":"VCB","price":90,"ts":5}]`},
		{"history oldest first", okMux, "/api/prices/FPT/history", http.StatusOK,
			`[{"symbol":"FPT","price":120,"ts":1},{"symbol":"FPT","price":121,"ts":2}]`},
		{"history limit not a number", okMux, "/api/prices/FPT/history?limit=abc", http.StatusBadRequest,
			`{"error":"limit must be between 1 and 1000"}`},
		{"history limit too large", okMux, "/api/prices/FPT/history?limit=5000", http.StatusBadRequest,
			`{"error":"limit must be between 1 and 1000"}`},
		{"history unknown symbol", okMux, "/api/prices/AAPL/history", http.StatusNotFound,
			`{"error":"unknown symbol"}`},
		{"get one symbol", okMux, "/api/prices/FPT", http.StatusOK,
			`{"symbol":"FPT","price":120,"ts":5}`},
		{"get symbol without price", okMux, "/api/prices/VNM", http.StatusNotFound,
			`{"error":"no price yet"}`},
		{"latest store error", brokenMux, "/api/prices", http.StatusInternalServerError,
			`{"error":"internal error"}`},
		{"history store error", brokenMux, "/api/prices/FPT/history", http.StatusInternalServerError,
			`{"error":"internal error"}`},
		{"get store error", brokenMux, "/api/prices/FPT", http.StatusInternalServerError,
			`{"error":"internal error"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// NewRequest tạo request "giả" trong bộ nhớ, không mở cổng mạng nào.
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			// NewRecorder là một http.ResponseWriter ghi lại status, header và body.
			rec := httptest.NewRecorder()

			tt.mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			if strings.Contains(rec.Body.String(), "10.0.0.5") {
				t.Errorf("response leaks internal error details: %s", rec.Body)
			}
			assertJSON(t, rec.Body.Bytes(), tt.wantBody)
		})
	}
}

// assertJSON decode cả hai phía vào kiểu any rồi so sánh, nên thứ tự key
// hay dấu xuống dòng cuối body không làm test fail.
func assertJSON(t *testing.T, got []byte, want string) {
	t.Helper() // khi fail, Go báo dòng gọi assertJSON thay vì dòng bên trong hàm này
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("response is not valid JSON: %v (body: %s)", err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("bad wantBody in test: %v", err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("body = %s, want %s", got, want)
	}
}
