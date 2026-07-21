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
	r.Post("/v1/auth/change-password", h.changePassword)
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

// ChangePasswordRequest is the change-password body.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// changePassword godoc
//
//	@Summary		Change your own password
//	@Description	Verifies the current password, then sets a new one. ALL other sessions are signed out — a password change is what someone does when they suspect a credential is compromised, so other refresh tokens must not survive it. A fresh token pair is returned, so the caller stays signed in on this device and should replace its stored tokens with these.
//	@Tags			auth
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ChangePasswordRequest	true	"current and new password"
//	@Success		200		{object}	TokenResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		401		{object}	map[string]string
//	@Router			/v1/auth/change-password [post]
func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	claims, ok := FromContext(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	var req ChangePasswordRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	resp, err := h.svc.ChangePassword(r.Context(), claims.TenantID, UserID(r.Context()),
		req.CurrentPassword, req.NewPassword, r.UserAgent())
	if err != nil {
		// A wrong current password is 401; a rejected NEW password is 400. They
		// are different problems and the form shows them in different places.
		if errors.Is(err, ErrWrongPassword) {
			httpx.Error(w, http.StatusUnauthorized, err.Error())
			return
		}
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
