package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestMux dựng router thật với Service dùng memStore (khai báo trong service_test.go),
// đã có sẵn user alice@example.com / password123.
func newTestMux(t *testing.T) *http.ServeMux {
	t.Helper()
	svc := newTestService()
	mustRegister(t, svc)
	mux := http.NewServeMux()
	NewHandler(svc, testTokens).Register(mux)
	return mux
}

func do(mux http.Handler, method, path, body, authHeader string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// login đăng nhập alice và trả về cặp token để dùng cho các test sau.
func login(t *testing.T, mux http.Handler) TokenPair {
	t.Helper()
	rec := do(mux, http.MethodPost, "/api/auth/login",
		`{"email":"alice@example.com","password":"password123"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", rec.Code, rec.Body)
	}
	var pair TokenPair
	if err := json.NewDecoder(rec.Body).Decode(&pair); err != nil {
		t.Fatal(err)
	}
	return pair
}

func TestRegisterHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"created", `{"email":"bob@example.com","username":"bob","password":"password123"}`, http.StatusCreated},
		{"invalid JSON", `{"email":`, http.StatusBadRequest},
		{"invalid input", `{"email":"bob","username":"bob","password":"password123"}`, http.StatusBadRequest},
		{"email taken", `{"email":"Alice@example.com","username":"bob","password":"password123"}`, http.StatusConflict},
		{"username taken", `{"email":"bob@example.com","username":"Alice","password":"password123"}`, http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestMux(t), http.MethodPost, "/api/auth/register", tt.body, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestRegisterHandlerHidesPasswordHash(t *testing.T) {
	rec := do(newTestMux(t), http.MethodPost, "/api/auth/register",
		`{"email":"bob@example.com","username":"bob","password":"password123"}`, "")
	if strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("response leaks password field: %s", rec.Body)
	}
}

func TestLoginHandler(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"ok", `{"email":"alice@example.com","password":"password123"}`, http.StatusOK},
		{"invalid JSON", `not json`, http.StatusBadRequest},
		{"missing fields", `{}`, http.StatusBadRequest},
		{"wrong password", `{"email":"alice@example.com","password":"wrong-password"}`, http.StatusUnauthorized},
		{"unknown email", `{"email":"nobody@example.com","password":"password123"}`, http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(newTestMux(t), http.MethodPost, "/api/auth/login", tt.body, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestRefreshHandler(t *testing.T) {
	mux := newTestMux(t)
	pair := login(t, mux)
	body := `{"refresh_token":"` + pair.RefreshToken + `"}`

	if rec := do(mux, http.MethodPost, "/api/auth/refresh", body, ""); rec.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", rec.Code, rec.Body)
	}
	// Token vừa dùng đã bị rotate nên lần thứ hai bị từ chối.
	if rec := do(mux, http.MethodPost, "/api/auth/refresh", body, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("reuse status = %d, want 401", rec.Code)
	}
	if rec := do(mux, http.MethodPost, "/api/auth/refresh", `{}`, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("empty token status = %d, want 401", rec.Code)
	}
	if rec := do(mux, http.MethodPost, "/api/auth/refresh", `{`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid JSON status = %d, want 400", rec.Code)
	}
}

func TestLogoutHandler(t *testing.T) {
	mux := newTestMux(t)
	pair := login(t, mux)
	body := `{"refresh_token":"` + pair.RefreshToken + `"}`

	// Gọi hai lần đều 204 (idempotent).
	for i := 0; i < 2; i++ {
		if rec := do(mux, http.MethodPost, "/api/auth/logout", body, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("logout #%d status = %d, want 204", i+1, rec.Code)
		}
	}
	// Sau logout, refresh token không còn dùng được.
	if rec := do(mux, http.MethodPost, "/api/auth/refresh", body, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout status = %d, want 401", rec.Code)
	}
}

func TestMeHandler(t *testing.T) {
	mux := newTestMux(t)
	pair := login(t, mux)

	rec := do(mux, http.MethodGet, "/api/auth/me", "", "Bearer "+pair.AccessToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var u User
	if err := json.NewDecoder(rec.Body).Decode(&u); err != nil {
		t.Fatal(err)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email = %q", u.Email)
	}

	if rec := do(mux, http.MethodGet, "/api/auth/me", "", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without token status = %d, want 401", rec.Code)
	}
	if rec := do(mux, http.MethodGet, "/api/auth/me", "", "Bearer garbage"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token status = %d, want 401", rec.Code)
	}
}
