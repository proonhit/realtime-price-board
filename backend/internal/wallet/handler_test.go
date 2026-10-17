package wallet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proonhit/realtime-price-board/backend/internal/auth"
)

// Middleware thật với secret riêng cho test, để kiểm tra luôn đường đi 401.
var testTokens = auth.Tokens{Secret: []byte("test-secret-at-least-32-bytes-long!!"), TTL: time.Minute}

func newTestMux() *http.ServeMux {
	mux := http.NewServeMux()
	NewHandler(NewService(newMemStore("alice", "bob")), auth.RequireAuth(testTokens)).Register(mux)
	return mux
}

func bearer(t *testing.T, userID string) string {
	t.Helper()
	tok, err := testTokens.IssueAccess(userID)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + tok
}

// do gửi request; headers là các cặp key, value (ví dụ "Idempotency-Key", "k1").
func do(mux http.Handler, method, path, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRequiresAuth(t *testing.T) {
	mux := newTestMux()
	tests := []struct{ method, path string }{
		{http.MethodGet, "/api/wallet"},
		{http.MethodPost, "/api/transfers"},
		{http.MethodGet, "/api/transfers"},
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := do(mux, tt.method, tt.path, `{"to_username":"bob","amount":1}`, "Idempotency-Key", "k1")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestGetWalletCreatesDemoBalance(t *testing.T) {
	rec := do(newTestMux(), http.MethodGet, "/api/wallet", "", "Authorization", bearer(t, "id-alice"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var w Wallet
	if err := json.NewDecoder(rec.Body).Decode(&w); err != nil {
		t.Fatal(err)
	}
	if w.Balance != DemoStartingBalance || w.Currency != "VND" {
		t.Fatalf("wallet = %+v", w)
	}
}

func TestTransferHandlerStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		key        string
		body       string
		wantStatus int
	}{
		{"created", "k1", `{"to_username":"bob","amount":50000}`, http.StatusCreated},
		{"missing Idempotency-Key", "", `{"to_username":"bob","amount":50000}`, http.StatusBadRequest},
		{"Idempotency-Key too long", strings.Repeat("k", 101), `{"to_username":"bob","amount":50000}`, http.StatusBadRequest},
		{"invalid JSON", "k1", `{"to_username":`, http.StatusBadRequest},
		// encoding/json không nhận các giá trị sau vào int64: tiền không bao giờ là số lẻ.
		{"fractional amount", "k1", `{"to_username":"bob","amount":1.5}`, http.StatusBadRequest},
		{"amount with .0", "k1", `{"to_username":"bob","amount":50000.0}`, http.StatusBadRequest},
		{"exponent amount", "k1", `{"to_username":"bob","amount":5e4}`, http.StatusBadRequest},
		{"string amount", "k1", `{"to_username":"bob","amount":"50000"}`, http.StatusBadRequest},
		{"amount overflows int64", "k1", `{"to_username":"bob","amount":99999999999999999999}`, http.StatusBadRequest},
		{"unknown field", "k1", `{"to_username":"bob","amout":50000}`, http.StatusBadRequest},
		{"zero amount", "k1", `{"to_username":"bob","amount":0}`, http.StatusBadRequest},
		{"negative amount", "k1", `{"to_username":"bob","amount":-1}`, http.StatusBadRequest},
		{"amount over max", "k1", `{"to_username":"bob","amount":1000000000001}`, http.StatusBadRequest},
		{"self transfer", "k1", `{"to_username":"Alice","amount":1}`, http.StatusBadRequest},
		{"recipient not found", "k1", `{"to_username":"nobody","amount":1}`, http.StatusNotFound},
		{"insufficient funds", "k1", `{"to_username":"bob","amount":1000001}`, http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			headers := []string{"Authorization", bearer(t, "id-alice")}
			if tt.key != "" {
				headers = append(headers, "Idempotency-Key", tt.key)
			}
			rec := do(newTestMux(), http.MethodPost, "/api/transfers", tt.body, headers...)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestTransferHandlerIdempotency(t *testing.T) {
	mux := newTestMux()
	tok := bearer(t, "id-alice")
	body := `{"to_username":"bob","amount":50000}`

	first := do(mux, http.MethodPost, "/api/transfers", body, "Authorization", tok, "Idempotency-Key", "k1")
	if first.Code != http.StatusCreated || first.Header().Get("Idempotent-Replayed") != "" {
		t.Fatalf("first: status = %d, replayed header = %q", first.Code, first.Header().Get("Idempotent-Replayed"))
	}
	var res1 TransferResult
	if err := json.NewDecoder(first.Body).Decode(&res1); err != nil {
		t.Fatal(err)
	}
	if res1.Balance != DemoStartingBalance-50_000 || res1.Transfer.Amount != 50_000 {
		t.Fatalf("first result = %+v", res1)
	}

	// Gửi lại y hệt: 200, có header Idempotent-Replayed, cùng transfer id, tiền không bị trừ thêm.
	again := do(mux, http.MethodPost, "/api/transfers", body, "Authorization", tok, "Idempotency-Key", "k1")
	if again.Code != http.StatusOK || again.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("replay: status = %d, replayed header = %q", again.Code, again.Header().Get("Idempotent-Replayed"))
	}
	var res2 TransferResult
	if err := json.NewDecoder(again.Body).Decode(&res2); err != nil {
		t.Fatal(err)
	}
	if res2.Transfer.ID != res1.Transfer.ID || res2.Balance != res1.Balance {
		t.Fatalf("replay result = %+v, want same as %+v", res2, res1)
	}

	// Cùng key, khác số tiền: 422.
	conflict := do(mux, http.MethodPost, "/api/transfers", `{"to_username":"bob","amount":1}`,
		"Authorization", tok, "Idempotency-Key", "k1")
	if conflict.Code != http.StatusUnprocessableEntity {
		t.Fatalf("conflict status = %d, want 422", conflict.Code)
	}

	// Key và idempotency_key/request_hash nội bộ không lộ ra JSON.
	if s := again.Body.String() + first.Body.String(); strings.Contains(s, "k1") || strings.Contains(s, "hash") {
		t.Fatalf("response leaks idempotency data: %s", s)
	}
}

func TestHistoryHandler(t *testing.T) {
	mux := newTestMux()
	alice, bob := bearer(t, "id-alice"), bearer(t, "id-bob")

	if rec := do(mux, http.MethodPost, "/api/transfers", `{"to_username":"bob","amount":100}`,
		"Authorization", alice, "Idempotency-Key", "k1"); rec.Code != http.StatusCreated {
		t.Fatalf("transfer status = %d", rec.Code)
	}

	rec := do(mux, http.MethodGet, "/api/transfers?limit=10", "", "Authorization", bob)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var items []HistoryItem
	if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Direction != "in" || items[0].Amount != 100 {
		t.Fatalf("bob history = %+v", items)
	}

	for _, q := range []string{"?limit=abc", "?limit=0x10", "?limit=101", "?limit=-1"} {
		if rec := do(mux, http.MethodGet, "/api/transfers"+q, "", "Authorization", bob); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s status = %d, want 400", q, rec.Code)
		}
	}
}
