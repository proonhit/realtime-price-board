package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/proonhit/realtime-price-board/backend/internal/platform/httpx"
)

type Handler struct {
	svc    *Service
	tokens Tokens
}

// NewHandler cần tokens để dựng middleware RequireAuth cho route /me.
func NewHandler(svc *Service, tokens Tokens) *Handler { return &Handler{svc: svc, tokens: tokens} }

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/auth/register", h.register)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/refresh", h.refresh)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.Handle("GET /api/auth/me", RequireAuth(h.tokens)(http.HandlerFunc(h.me)))
}

// decode đọc JSON body, giới hạn 4KB để client không gửi body khổng lồ làm tốn RAM.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(v); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}

	user, err := h.svc.Register(r.Context(), body.Email, body.Username, body.Password)
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrEmailTaken), errors.Is(err, ErrUsernameTaken):
		httpx.Error(w, http.StatusConflict, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusCreated, user)
	}
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}

	pair, err := h.svc.Login(r.Context(), body.Email, body.Password)
	switch {
	case errors.Is(err, ErrInvalidInput):
		httpx.Error(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrInvalidCredentials):
		httpx.Error(w, http.StatusUnauthorized, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusOK, pair)
	}
}

type refreshBody struct {
	RefreshToken string `json:"refresh_token"`
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var body refreshBody
	if !decode(w, r, &body) {
		return
	}

	pair, err := h.svc.Refresh(r.Context(), body.RefreshToken)
	switch {
	case errors.Is(err, ErrInvalidToken):
		httpx.Error(w, http.StatusUnauthorized, err.Error())
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusOK, pair)
	}
}

// logout không cần access token: refresh token trong body chính là thứ cần thu hồi.
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var body refreshBody
	if !decode(w, r, &body) {
		return
	}
	if err := h.svc.Logout(r.Context(), body.RefreshToken); err != nil {
		httpx.Internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFrom(r.Context())
	if !ok {
		// Chỉ xảy ra nếu quên bọc route bằng RequireAuth.
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.svc.Me(r.Context(), userID)
	switch {
	case errors.Is(err, ErrUserNotFound):
		// Token còn hạn nhưng user đã bị xoá.
		httpx.Error(w, http.StatusUnauthorized, "unauthorized")
	case err != nil:
		httpx.Internal(w, err)
	default:
		httpx.JSON(w, http.StatusOK, user)
	}
}
