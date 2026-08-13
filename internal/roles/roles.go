// Package roles implements per-account custom roles (RBAC): a company admin
// defines roles with a granular permission set, and assigns each user one.
//
// Phase 1 is additive — it stores/serves roles and the user's effective
// permissions, but does not yet gate endpoints (that's RequirePermission, next).
// A user with no assigned role falls back to the legacy rank, so nothing breaks.
package roles

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/perms"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler serves role management + the permission endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the endpoints inside the authenticated group.
func (h *Handler) Routes(r chi.Router) {
	// Managing roles + assigning them is a company-admin action.
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Get("/v1/permissions/catalog", h.catalog)
		ar.Get("/v1/roles", h.list)
		ar.Post("/v1/roles", h.create)
		ar.Put("/v1/roles/{id}", h.update)
		ar.Delete("/v1/roles/{id}", h.del)
		ar.Put("/v1/users/{id}/role", h.assign)
	})
	// Any authenticated user reads their OWN effective permissions (for UI gating).
	r.Get("/v1/me/permissions", h.mine)
}

// DTO is a role as returned to the client.
type DTO struct {
	ID          int64    `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// catalog: the full permission matrix (for the role editor).
func (h *Handler) catalog(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, perms.Catalog)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid := auth.TenantID(r.Context())
	out := []DTO{}
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT id, name, permissions FROM roles WHERE tenant_id=$1 ORDER BY name`, tid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d DTO
			if err := rows.Scan(&d.ID, &d.Name, &d.Permissions); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list roles failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

type writeReq struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req writeReq
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	tid := auth.TenantID(r.Context())
	perm := perms.Filter(req.Permissions)
	var d DTO
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`INSERT INTO roles (tenant_id, name, permissions) VALUES ($1,$2,$3) RETURNING id, name, permissions`,
			tid, req.Name, perm).Scan(&d.ID, &d.Name, &d.Permissions)
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "create role failed (name may already exist)")
		return
	}
	httpx.JSON(w, http.StatusCreated, d)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req writeReq
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	tid := auth.TenantID(r.Context())
	perm := perms.Filter(req.Permissions)
	var d DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`UPDATE roles SET name=$2, permissions=$3, updated_at=now() WHERE id=$1 AND tenant_id=$4 RETURNING id, name, permissions`,
			id, req.Name, perm, tid).Scan(&d.ID, &d.Name, &d.Permissions)
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "update role failed")
		return
	}
	httpx.JSON(w, http.StatusOK, d)
}

func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	tid := auth.TenantID(r.Context())
	// ON DELETE SET NULL detaches any users (they fall back to the legacy rank).
	if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM roles WHERE id=$1 AND tenant_id=$2`, id, tid)
		return err
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete role failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// assign sets (or clears, role_id=null) a user's custom role.
func (h *Handler) assign(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		RoleID *int64 `json:"role_id"`
	}
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	tid := auth.TenantID(r.Context())
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		// Both user and role are RLS-scoped to the tenant, so a cross-tenant id
		// simply matches nothing.
		ct, err := tx.Exec(r.Context(),
			`UPDATE users SET role_id=$2, updated_at=now()
			 WHERE id=$1 AND ($2::bigint IS NULL OR EXISTS (SELECT 1 FROM roles WHERE id=$2))`, uid, req.RoleID)
		if err != nil {
			return err
		}
		if ct.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "assign role failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mine returns the caller's effective permission keys: their role's permissions
// intersected with the account's enabled features. Fallbacks that keep everyone
// working during the migration:
//   - platform_admin  → every permission
//   - no role assigned → every permission (legacy rank still governs via RequireRole)
func (h *Handler) mine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if c, ok := auth.FromContext(ctx); ok && auth.AtLeast(c.Role, auth.RolePlatformAdmin) {
		httpx.JSON(w, http.StatusOK, allKeys())
		return
	}
	tid := auth.TenantID(ctx)
	uid := auth.UserID(ctx)

	var rolePerms []string
	hasRole := false
	disabled := map[string]bool{}
	err := db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
		// the user's role permissions (if any)
		err := tx.QueryRow(ctx, `SELECT r.permissions FROM users u JOIN roles r ON r.id = u.role_id WHERE u.id=$1`, uid).Scan(&rolePerms)
		if err == nil {
			hasRole = true
		} else if err != pgx.ErrNoRows {
			return err
		}
		// account feature overrides (enabled=false rows)
		rows, err := tx.Query(ctx, `SELECT feature_key FROM tenant_features WHERE tenant_id=$1 AND enabled=false`, tid)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				return err
			}
			disabled[k] = true
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read permissions failed")
		return
	}

	if !hasRole {
		httpx.JSON(w, http.StatusOK, allKeys()) // legacy fallback
		return
	}
	// keep only granted perms whose feature is enabled for the account
	feat := map[string]string{}
	for _, p := range perms.Catalog {
		feat[p.Key] = p.Feature
	}
	granted := map[string]bool{}
	for _, k := range rolePerms {
		granted[k] = true
	}
	out := []string{}
	for _, p := range perms.Catalog {
		if granted[p.Key] && (p.Feature == "" || !disabled[p.Feature]) {
			out = append(out, p.Key)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

func allKeys() []string {
	out := make([]string, 0, len(perms.Catalog))
	for _, p := range perms.Catalog {
		out = append(out, p.Key)
	}
	return out
}
