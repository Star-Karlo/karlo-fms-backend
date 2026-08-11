package auth

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/karlo/fms-backend/internal/platform/httpx"
)

type ctxKey struct{}

// WithClaims returns a copy of ctx carrying the authenticated claims.
func WithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// FromContext extracts the authenticated claims, if present.
func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(ctxKey{}).(*Claims)
	return c, ok
}

// UserID returns the authenticated user id (0 if unauthenticated).
func UserID(ctx context.Context) int64 {
	if c, ok := FromContext(ctx); ok {
		if id, err := strconv.ParseInt(c.Subject, 10, 64); err == nil {
			return id
		}
	}
	return 0
}

// TenantID returns the authenticated tenant id (0 if unauthenticated).
func TenantID(ctx context.Context) int64 {
	if c, ok := FromContext(ctx); ok {
		return c.TenantID
	}
	return 0
}

// EffectiveTenant is the tenant a request operates on. Normally the caller's own
// tenant — but a platform_admin may target ANOTHER account by passing
// ?account_id=N (carried from the cross-tenant lists). Ignored for everyone else,
// so a normal user can never reach outside their own tenant. RLS still applies to
// whatever tenant this returns, so a bad id simply sees nothing.
func EffectiveTenant(r *http.Request) int64 {
	tid := TenantID(r.Context())
	v := r.URL.Query().Get("account_id")
	if v == "" {
		return tid
	}
	c, ok := FromContext(r.Context())
	if !ok || c.Role != RolePlatformAdmin {
		return tid
	}
	if aid, err := strconv.ParseInt(v, 10, 64); err == nil && aid > 0 {
		return aid
	}
	return tid
}

// Authenticator verifies the Bearer access token and injects claims into the
// request context. Requests without a valid token are rejected with 401.
func Authenticator(tm *TokenManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := tm.ParseAccess(strings.TrimPrefix(h, "Bearer "))
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}

// RequireRole rejects requests whose user does not meet the minimum role.
func RequireRole(min string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := FromContext(r.Context())
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "unauthenticated")
				return
			}
			if !AtLeast(c.Role, min) {
				httpx.Error(w, http.StatusForbidden, "insufficient role")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
