// Package roles implements per-account custom roles (RBAC): a company admin
// defines roles with a granular permission set, and assigns each user one.
//
// Phase 1 is additive — it stores/serves roles and the user's effective
// permissions, but does not yet gate endpoints (that's RequirePermission, next).
// A user with no assigned role falls back to the legacy rank, so nothing breaks.
package roles

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler serves role management + the permission endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Require blocks (403) a request whose user's assigned role lacks `perm`.
// Pass-through cases keep everyone working during the migration:
//   - platform_admin           → always allowed
//   - user with NO custom role → allowed (legacy rank still governs)
//
// So enforcement only bites once a user is given a custom role.
func (h *Handler) Require(perm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, ok := auth.FromContext(r.Context()); ok && auth.AtLeast(c.Role, auth.RolePlatformAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			if allowed(r, h, perm) {
				next.ServeHTTP(w, r)
				return
			}
			httpx.Error(w, http.StatusForbidden, "your role does not permit this")
		})
	}
}

// RequireRW is method-aware: safe reads (GET/HEAD) need viewPerm, writes
// (POST/PUT/PATCH/DELETE) need editPerm. Same pass-through rules as Require, so a
// role can grant "see this" without "change this".
func (h *Handler) RequireRW(viewPerm, editPerm string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, ok := auth.FromContext(r.Context()); ok && auth.AtLeast(c.Role, auth.RolePlatformAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			perm := viewPerm
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				perm = editPerm
			}
			if allowed(r, h, perm) {
				next.ServeHTTP(w, r)
				return
			}
			httpx.Error(w, http.StatusForbidden, "your role does not permit this")
		})
	}
}

// userHasPerm reports whether the user's assigned role grants perm. No custom
// role (ErrNoRows) or a lookup error fails OPEN — the legacy rank still applies
// and an error must not lock the app.
func (h *Handler) userHasPerm(ctx context.Context, tenantID, userID int64, perm string) bool {
	var ps []string
	err := db.Tx(ctx, h.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT r.permissions FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id = $1`, userID).Scan(&ps)
	})
	if err == pgx.ErrNoRows || err != nil {
		return true
	}
	for _, p := range ps {
		if p == perm {
			return true
		}
	}
	return false
}

// DTO is a role as returned to the client.
type DTO struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// allowed decides a permission from whichever source is authoritative for this
// caller.
//
// A token minted by the shared authentication service already carries the
// answer that service resolved — role permissions unioned with any per-person
// extras, intersected with the company's entitlement. Re-deriving it here from
// fms_app.roles would consult a table that stopped deciding anything when roles
// moved, and would disagree with what the token itself asserts.
//
// A locally minted token carries nothing, so the local table is still read.
// That is what keeps a deployment that has not cut over working unchanged.
func allowed(r *http.Request, h *Handler, perm string) bool {
	if c, ok := auth.FromContext(r.Context()); ok && c.FromSharedIAM() {
		return c.HasPerm(perm)
	}
	return h.userHasPerm(r.Context(), auth.TenantID(r.Context()), auth.UserID(r.Context()), perm)
}
