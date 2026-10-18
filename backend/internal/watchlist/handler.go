package watchlist

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/proonhit/realtime-price-board/backend/internal/auth"
	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

type Handler struct {
	svc         *Service
	requireAuth func(http.Handler) http.Handler
}

// NewHandler nhận sẵn middleware xác thực thay vì nhận auth.Tokens. Nhờ vậy
// watchlist không cần biết token là JWT hay gì khác, không cần secret, và test
// có thể truyền một middleware giả gắn user id tuỳ ý.
// Watchlist chỉ phụ thuộc auth.UserIDFrom để đọc user id mà middleware đã gắn.
func NewHandler(svc *Service, requireAuth func(http.Handler) http.Handler) *Handler {
	return &Handler{svc: svc, requireAuth: requireAuth}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/watchlist", h.requireAuth(http.HandlerFunc(h.list)))
	mux.Handle("POST /api/watchlist", h.requireAuth(http.HandlerFunc(h.add)))
	mux.Handle("DELETE /api/watchlist/{symbol}", h.requireAuth(http.HandlerFunc(h.remove)))
}

// userID đọc user id do middleware gắn vào context. Không có (quên bọc route
// bằng middleware) thì trả 401 luôn, không bao giờ rơi về một danh sách chung.
func userID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := auth.UserIDFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	}
	return id, ok
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	syms, err := h.svc.List(r.Context(), uid)
	if err != nil {
		httpx.Internal(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, syms)
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	var body struct {
		Symbol string `json:"symbol"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	syms, err := h.svc.Add(r.Context(), uid, body.Symbol)
	switch {
	case errors.Is(err, price.ErrUnknownSymbol):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusOK, syms)
	}
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	uid, ok := userID(w, r)
	if !ok {
		return
	}
	err := h.svc.Remove(r.Context(), uid, r.PathValue("symbol"))
	switch {
	case errors.Is(err, price.ErrUnknownSymbol), errors.Is(err, ErrNotInWatchlist):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
