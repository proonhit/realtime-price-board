package watchlist

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
	"github.com/proonhit/realtime-price-board/backend/internal/price"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/watchlist", h.list)
	mux.HandleFunc("POST /api/watchlist", h.add)
	mux.HandleFunc("DELETE /api/watchlist/{symbol}", h.remove)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	syms, err := h.svc.List(r.Context())
	if err != nil {
		httpx.Internal(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, syms)
}

func (h *Handler) add(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Symbol string `json:"symbol"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	syms, err := h.svc.Add(r.Context(), body.Symbol)
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
	err := h.svc.Remove(r.Context(), r.PathValue("symbol"))
	switch {
	case errors.Is(err, price.ErrUnknownSymbol), errors.Is(err, ErrNotInWatchlist):
		httpx.Error(w, http.StatusNotFound, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
