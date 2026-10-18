package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequireAuth(t *testing.T) {
	valid, err := testTokens.IssueAccess("user-42")
	if err != nil {
		t.Fatal(err)
	}
	expired, err := Tokens{Secret: testTokens.Secret, TTL: -time.Minute}.IssueAccess("user-42")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{"missing header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic dXNlcjpwYXNz", http.StatusUnauthorized},
		{"bearer without token", "Bearer ", http.StatusUnauthorized},
		{"bad token", "Bearer not-a-jwt", http.StatusUnauthorized},
		{"expired token", "Bearer " + expired, http.StatusUnauthorized},
		{"valid token", "Bearer " + valid, http.StatusOK},
		{"scheme is case-insensitive", "bearer " + valid, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotID string
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotID, _ = UserIDFrom(r.Context())
				w.WriteHeader(http.StatusOK)
			})

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			RequireAuth(testTokens)(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusOK && gotID != "user-42" {
				t.Fatalf("handler saw user id %q, want user-42", gotID)
			}
			if tt.wantStatus == http.StatusUnauthorized && gotID != "" {
				t.Fatal("next handler must not run when unauthorized")
			}
		})
	}
}

func TestUserIDFromEmptyContext(t *testing.T) {
	if _, ok := UserIDFrom(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("expected ok = false without RequireAuth")
	}
}
