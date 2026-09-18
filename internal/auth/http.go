package auth

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler exposes the auth endpoints.
type Handler struct {
	svc *Service
	// up, when set, makes login and refresh DELEGATE to the shared
	// authentication service instead of checking this service's own users
	// table. Nil means no upstream is configured and the local login stands.
	up *Upstream
	// sso verifies tokens from that service, and is used here to read the role
	// out of a token just issued so the login response can report it.
	sso *SSO
	// cookieDomain, when set, switches the refresh token to an httpOnly
	// cookie scoped to that domain (e.g. ".karlo.id" so fms.karlo.id and
	// fms-api.karlo.id share it). The token then never reaches JavaScript —
	// an XSS can still call the API while the tab is open, but cannot steal a
	// session that outlives it. Empty = the token travels in the JSON body
	// (local development, where there is no shared domain).
	cookieDomain string
}

// NewHandler builds the auth HTTP handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// WithUpstream routes login and refresh to the shared authentication service.
func (h *Handler) WithUpstream(up *Upstream, sso *SSO) *Handler { h.up, h.sso = up, sso; return h }

// WithCookieDomain enables httpOnly refresh cookies for that domain.
func (h *Handler) WithCookieDomain(d string) *Handler { h.cookieDomain = d; return h }

// The SSO cookies, shared with TMS and the auth service (pinned 2026-09-18):
// karlo_rt carries the refresh token, HttpOnly, Domain .karlo.id, Path /, so
// whichever app logs in or refreshes, both read the same current token;
// karlo_session is a JS-visible marker (value "1", no token) both SPAs poll
// for the instant cross-app logout. fms_refresh is the pre-SSO name, still
// read for one release so nobody is logged out by the rename.
const (
	refreshCookie  = "karlo_rt"
	sessionCookie  = "karlo_session"
	legacyCookie   = "fms_refresh"
	cookieMaxAge   = 30 * 24 * 3600
	cookieDomainRT = "/"
)

// issue writes a token response, moving the refresh token into the cookie
// when cookie mode is on so the body never carries it.
func (h *Handler) issue(w http.ResponseWriter, resp *TokenResponse) {
	if h.cookieDomain != "" && resp != nil && resp.RefreshToken != "" {
		http.SetCookie(w, &http.Cookie{
			Name: refreshCookie, Value: resp.RefreshToken, Domain: h.cookieDomain, Path: cookieDomainRT,
			MaxAge: cookieMaxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
		})
		// The marker carries WHO is signed in (the platform user id), so a tab
		// open as one person notices when another person signs in elsewhere
		// on the same browser and re-adopts the session instead of keeping
		// its stale identity. Falls back to the local id, then "1".
		who := h.sso.PlatformUserID(resp.AccessToken)
		if who == "" && resp.User.ID != 0 {
			who = strconv.FormatInt(resp.User.ID, 10)
		}
		if who == "" {
			who = "1"
		}
		http.SetCookie(w, &http.Cookie{
			Name: sessionCookie, Value: who, Domain: h.cookieDomain, Path: cookieDomainRT,
			MaxAge: cookieMaxAge, HttpOnly: false, Secure: true, SameSite: http.SameSiteLaxMode,
		})
		// The pre-SSO cookie must not linger: with karlo_rt present a stale
		// fms_refresh from a previous user could otherwise win a refresh.
		http.SetCookie(w, &http.Cookie{Name: legacyCookie, Value: "", Domain: h.cookieDomain, Path: "/v1/auth", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		resp.RefreshToken = ""
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// refreshTokenFrom prefers the body (local mode, or an explicit token) and
// falls back to the cookie.
func (h *Handler) refreshTokenFrom(r *http.Request, body string) string {
	if body != "" {
		return body
	}
	for _, name := range []string{refreshCookie, legacyCookie} {
		if c, err := r.Cookie(name); err == nil && c.Value != "" {
			return c.Value
		}
	}
	return ""
}

func (h *Handler) clearCookie(w http.ResponseWriter) {
	if h.cookieDomain == "" {
		return
	}
	for _, c := range []struct {
		name, path string
		httpOnly   bool
	}{{refreshCookie, cookieDomainRT, true}, {sessionCookie, cookieDomainRT, false}, {legacyCookie, "/v1/auth", true}} {
		http.SetCookie(w, &http.Cookie{Name: c.name, Value: "", Domain: h.cookieDomain, Path: c.path, MaxAge: -1, HttpOnly: c.httpOnly, Secure: true, SameSite: http.SameSiteLaxMode})
	}
}

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
	if h.up != nil {
		resp, up, err := h.up.Login(r.Context(), req.Email, req.Password)
		// Fill in who just signed in. Without this the browser gets a session
		// with no role and every role-gated control looks the same for
		// everybody — no edit buttons, no admin screens, and no error to
		// explain it.
		if err == nil && resp != nil && h.sso != nil && up != nil {
			resp.User = h.sso.UserFromToken(resp.AccessToken, up.Email, up.FullName)
		}
		h.writeUpstream(w, resp, err)
		return
	}
	resp, err := h.svc.Login(r.Context(), req.Email, req.Password, r.UserAgent())
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.issue(w, resp)
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
	_ = httpx.DecodeJSON(r, &req) // body may be {} in cookie mode
	token := h.refreshTokenFrom(r, req.RefreshToken)
	if token == "" {
		httpx.Error(w, http.StatusUnauthorized, "no refresh token")
		return
	}
	if h.up != nil {
		resp, err := h.up.Refresh(r.Context(), token)
		if err != nil {
			h.clearCookie(w)
		}
		h.writeUpstream(w, resp, err)
		return
	}
	resp, err := h.svc.Refresh(r.Context(), token, r.UserAgent())
	if err != nil {
		h.clearCookie(w)
		httpx.Error(w, http.StatusUnauthorized, err.Error())
		return
	}
	h.issue(w, resp)
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
	token := h.refreshTokenFrom(r, req.RefreshToken)
	h.clearCookie(w)
	if h.up != nil {
		// The session lives upstream now, so revoking it here would leave the
		// token working: this service holds no record of it to revoke.
		if _, _, err := h.up.Forward(r.Context(), http.MethodPost, "/api/v1/auth/logout",
			bearerOf(r), map[string]string{"refreshToken": token}); err != nil {
			log.Printf("auth upstream logout: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := h.svc.Logout(r.Context(), token); err != nil {
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
	if h.up != nil {
		// The profile lives upstream. Reading it from this service's own row
		// would go stale the moment somebody edits their name in the shared
		// IAM — and would fail outright for Karlo staff, who legitimately have
		// no row here at all.
		//
		// The RESPONSE is mapped back into this service's shape rather than
		// passed through, because /v1/auth/me is what the web app reads to
		// decide who is signed in, and changing its field names would be a
		// frontend change for no gain.
		dto, err := h.up.Me(r.Context(), bearerOf(r), claims)
		if err != nil {
			log.Printf("auth upstream me: %v", err)
			httpx.Error(w, http.StatusBadGateway, "the authentication service is unavailable")
			return
		}
		httpx.JSON(w, http.StatusOK, dto)
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
	if h.up != nil {
		// Changing it here would change a hash nothing authenticates against
		// any more — the user would see "password updated" and then find the
		// old one still works and the new one does not.
		code, raw, err := h.up.Forward(r.Context(), http.MethodPost, "/api/v1/auth/change-password",
			bearerOf(r), map[string]string{
				"currentPassword": req.CurrentPassword,
				"newPassword":     req.NewPassword,
			})
		if err != nil {
			log.Printf("auth upstream change-password: %v", err)
			httpx.Error(w, http.StatusBadGateway, "the authentication service is unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write(raw)
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

// writeUpstream renders a delegated login or refresh.
//
// The two failure kinds are kept apart on purpose. A refusal is the caller's
// problem and a 401; anything else — the auth service down, slow, or answering
// something unparseable — is THIS deployment's problem and a 502. Collapsing
// them into 401 would tell somebody their password is wrong during an outage,
// and send them to reset a credential that was never at fault.
func (h *Handler) writeUpstream(w http.ResponseWriter, resp *TokenResponse, err error) {
	switch {
	case err == nil:
		h.issue(w, resp)
	case errors.Is(err, ErrUpstreamRejected):
		httpx.Error(w, http.StatusUnauthorized, "invalid email or password")
	case errors.Is(err, ErrUpstreamThrottled):
		// The auth service locks an account after 5 failed logins for 15
		// minutes and counts a request during the lockout as an attempt —
		// so the honest advice is to stop, not retry.
		httpx.Error(w, http.StatusTooManyRequests, "Too many attempts. The account is locked for 15 minutes after 5 wrong passwords — wait, don't retry (each retry restarts the count).")
	default:
		log.Printf("auth upstream: %v", err)
		httpx.Error(w, http.StatusBadGateway, "the authentication service is unavailable")
	}
}

// bearerOf returns the caller's raw token, so a forwarded request carries the
// caller's own authority and not this service's.
func bearerOf(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}
