package auth

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler exposes the auth endpoints.
type Handler struct {
	svc *Service
}

// NewHandler builds the auth HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// PublicRoutes mounts the unauthenticated auth endpoints.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Post("/v1/auth/login", h.login)
	r.Post("/v1/auth/refresh", h.refresh)
}

// PrivateRoutes mounts the authenticated auth endpoints.
func (h *Handler) PrivateRoutes(r chi.Router) {
	r.Post("/v1/auth/logout", h.logout)
	r.Get("/v1/auth/me", h.me)
}

// login godoc
//
//	@Summary	Log in with email + password
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		LoginRequest	true	"credentials"
//	@Success	200		{object}	TokenResponse
//	@Failure	401		{object}	map[string]string
//	@Router		/v1/auth/login [post]
func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	resp, err := h.svc.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// refresh godoc
//
//	@Summary	Rotate a refresh token for a new token pair
//	@Tags		auth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		RefreshRequest	true	"refresh token"
//	@Success	200		{object}	TokenResponse
//	@Failure	401		{object}	map[string]string
//	@Router		/v1/auth/refresh [post]
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	resp, err := h.svc.Refresh(r.Context(), req.RefreshToken, r.UserAgent())
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// logout godoc
//
//	@Summary	Revoke a refresh token
//	@Tags		auth
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body	RefreshRequest	true	"refresh token to revoke"
//	@Success	204
//	@Router		/v1/auth/logout [post]
func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	var req RefreshRequest
	_ = httpx.DecodeJSON(r, &req)
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "logout failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// me godoc
//
//	@Summary	Current authenticated user
//	@Tags		auth
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	UserDTO
//	@Failure	401	{object}	map[string]string
//	@Router		/v1/auth/me [get]
func (h *Handler) me(w http.ResponseWriter, r *http.Request) {
	claims, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	dto, err := h.svc.Me(r.Context(), claims.TenantID, UserID(r.Context()))
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
			return
		}
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}
