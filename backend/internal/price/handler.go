package price

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prices", h.latest)
	mux.HandleFunc("GET /api/prices/{symbol}/history", h.history)
}

func (h *Handler) latest(w http.ResponseWriter, r *http.Request) {
	ticks, err := h.svc.Latest(r.Context())
	if err != nil {
		httpx.Internal(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ticks)
}

func (h *Handler) history(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, ErrInvalidLimit.Error())
			return
		}
		limit = n
	}

	ticks, err := h.svc.History(r.Context(), r.PathValue("symbol"), limit)
	switch {
	case errors.Is(err, ErrUnknownSymbol):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidLimit):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusOK, ticks)
	}
}
