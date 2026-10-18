package wallet

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/proonhit/realtime-price-board/backend/internal/auth"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
)

type Handler struct {
	svc         *Service
	requireAuth func(http.Handler) http.Handler
}

// NewHandler nhận middleware xác thực đã dựng sẵn (giống watchlist), nên package
// wallet không cần biết gì về JWT.
func NewHandler(svc *Service, requireAuth func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, requireAuth: requireAuth}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/wallet", h.requireAuth(http.HandlerFunc(h.wallet)))
	mux.Handle("POST /api/transfers", h.requireAuth(http.HandlerFunc(h.transfer)))
	mux.Handle("GET /api/transfers", h.requireAuth(http.HandlerFunc(h.history)))
}

// userID đọc user id do middleware gắn; thiếu (quên bọc route) thì trả 401.
func userID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	}
	return id, ok
}

// writeError đổi lỗi nghiệp vụ thành HTTP status, dùng chung cho cả ba route.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput), errors.Is(err, ErrInvalidIdempotencyKey), errors.Is(err, ErrSelfTransfer):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrRecipientNotFound):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInsufficientFunds), errors.Is(err, ErrIdempotencyConflict):
		// 422: request đúng cú pháp nhưng không thực hiện được. Với key dùng lại
		// cho nội dung khác, chọn 422 theo bản nháp IETF "Idempotency-Key HTTP Header";
		// 409 được dành cho trường hợp request trước cùng key vẫn đang chạy dở.
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrUserNotFound):
		// Token còn hạn nhưng user đã bị xoá.
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	default:
		httpx.Internal(w, err)
	}
}

func (h *Handler) wallet(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	wal, err := h.svc.Wallet(r.Context(), uid)
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, wal)
}

func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}

	var body struct {
		ToUsername string `json:"to_username"`
		// int64: encoding/json từ chối 1.5, 1e3, "50000" hay số vượt int64 ngay lúc
		// decode, nên tiền không bao giờ đi qua float.
		Amount int64 `json:"amount"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10))
	// Gõ sai tên field (ví dụ "amout") thì báo lỗi luôn, thay vì coi như amount = 0.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body: amount must be an integer number of dong")
		return
	}

	res, replayed, err := h.svc.Transfer(r.Context(), uid, TransferRequest{
		ToUsername:     body.ToUsername,
		Amount:         body.Amount,
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, err)
		return
	}
	if replayed {
		// Giống Stripe: header này cho client biết đây là kết quả lưu từ lần trước,
		// tiền không bị chuyển thêm. Lần đầu là 201 Created, lần gửi lại là 200 OK.
		w.Header().Set("Idempotent-Replayed", "true")
		httpx.JSON(w, http.StatusOK, res)
		return
	}
	httpx.JSON(w, http.StatusCreated, res)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "limit must be an integer")
			return
		}
		limit = n
	}
	items, err := h.svc.History(r.Context(), uid, limit)
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, items)
}
