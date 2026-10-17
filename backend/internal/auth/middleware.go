package auth

import (
	"context"
	"net/http"
	"strings"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
)

// ctxKey là key riêng của package để gắn user id vào context. Dùng kiểu struct
// chưa export thay vì string để package khác không thể vô tình ghi đè cùng key.
type ctxKey struct{}

// RequireAuth chỉ cho request đi tiếp khi header có "Authorization: Bearer <access token>"
// hợp lệ. User id lấy từ token được gắn vào context, handler đọc lại bằng UserIDFrom.
func RequireAuth(t Tokens) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
			if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" {
				unauthorized(w, "missing bearer token")
				return
			}
			userID, err := t.ParseAccess(token)
			if err != nil {
				// Không trả chi tiết lỗi (hết hạn, sai chữ ký...) cho client.
				unauthorized(w, "invalid or expired token")
				return
			}
			ctx := context.WithValue(r.Context(), ctxKey{}, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserIDFrom trả user id mà RequireAuth đã gắn; ok = false nếu route không qua RequireAuth.
func UserIDFrom(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(ctxKey{}).(string)
	return id, ok
}

func unauthorized(w http.ResponseWriter, msg string) {
	// Header chuẩn (RFC 6750) báo client biết cần gửi Bearer token.
	w.Header().Set("WWW-Authenticate", "Bearer")
	httpx.Error(w, http.StatusUnauthorized, msg)
}
