// Package features implements per-tenant, module-level feature entitlements.
//
// A platform admin toggles which modules each customer may use. Enforcement is
// two-layer: the frontend hides disabled modules, and RequireFeature returns 403
// on the API so a disabled module can't be reached by calling it directly.
//
// Default is OPT-OUT: a tenant with no explicit row for a feature has it ENABLED,
// so existing customers keep everything until an admin turns something off.
package features

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Def is one gateable feature. Parent ("" for top-level) nests a sub-feature
// under another, so the admin editor mirrors the app's sidebar hierarchy. Order
// in Registry is the display order.
type Def struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Parent string `json:"parent"`
}

// Registry is the canonical, ordered list of toggleable features, arranged as
// parent → sub-feature to match the sidebar. Settings and tenant/platform admin
// are intentionally absent (always available so an admin can't lock a customer
// out of their own account).
var Registry = []Def{
	{Key: "dashboard", Label: "Dashboard"},
	{Key: "ai_dashboard", Label: "AI Dashboard", Parent: "dashboard"},

	{Key: "live", Label: "Live View"},
	{Key: "geofence", Label: "Geofence", Parent: "live"},

	{Key: "controltower", Label: "Control Tower"},
	{Key: "notifications", Label: "Notifications"},

	{Key: "share_link", Label: "Share Link"},
	{Key: "generate_link", Label: "Generate Link", Parent: "share_link"},

	{Key: "reports", Label: "Reporting"},
	{Key: "custom_report", Label: "Custom Report", Parent: "reports"},
	{Key: "ai_reporting", Label: "AI Reporting", Parent: "reports"},

	{Key: "camera", Label: "Camera (TrackVision)"},

	{Key: "fleet_management", Label: "Fleet Management"},
	{Key: "maintenance", Label: "Maintenance", Parent: "fleet_management"},
	{Key: "hangars", Label: "Workshops", Parent: "fleet_management"},
	{Key: "license", Label: "License", Parent: "fleet_management"},

	{Key: "master_data", Label: "Master Data"},
	{Key: "dashcams", Label: "Dashcam", Parent: "master_data"},
	{Key: "rfid", Label: "RFID", Parent: "master_data"},
	{Key: "users", Label: "Users", Parent: "master_data"},

	{Key: "marketplace", Label: "Marketplace"},
}

func valid(key string) bool {
	for _, d := range Registry {
		if d.Key == key {
			return true
		}
	}
	return false
}

// Handler serves the entitlement endpoints and provides the gate middleware.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the entitlement endpoints inside the authenticated group.
func (h *Handler) Routes(r chi.Router) {
	// Managing another tenant's entitlements is platform-admin only.
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RolePlatformAdmin))
		ar.Get("/v1/tenants/{id}/features", h.getForTenant)
		ar.Put("/v1/tenants/{id}/features", h.setForTenant)
	})
	// Any authenticated user may read their OWN tenant's effective features, so
	// the app can hide disabled modules.
	r.Get("/v1/me/features", h.getMine)
}

// overrides returns the explicit tenant_features rows for a tenant (key->enabled).
// `tenant_features` has no RLS, so this reads directly through the pool — the same
// pattern the tenants handler uses for cross-tenant management.
func (h *Handler) overrides(ctx context.Context, tenantID int64) (map[string]bool, error) {
	rows, err := h.pool.Query(ctx, `SELECT feature_key, enabled FROM tenant_features WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		var en bool
		if err := rows.Scan(&k, &en); err != nil {
			return nil, err
		}
		out[k] = en
	}
	return out, rows.Err()
}

// effective merges the registry (all-on default) with the tenant's overrides.
func effective(ov map[string]bool) map[string]bool {
	out := make(map[string]bool, len(Registry))
	for _, d := range Registry {
		if v, ok := ov[d.Key]; ok {
			out[d.Key] = v
		} else {
			out[d.Key] = true // opt-out default
		}
	}
	return out
}

// Enabled reports whether a tenant has a feature on (default true). Unknown keys
// are treated as enabled so a gate on an unregistered key never hard-blocks.
func (h *Handler) Enabled(ctx context.Context, tenantID int64, key string) bool {
	if !valid(key) {
		return true
	}
	var enabled bool
	err := h.pool.QueryRow(ctx, `SELECT enabled FROM tenant_features WHERE tenant_id = $1 AND feature_key = $2`, tenantID, key).Scan(&enabled)
	if err == pgx.ErrNoRows {
		return true // no explicit row = enabled
	}
	if err != nil {
		return true // fail OPEN: an entitlement lookup error must not break the app
	}
	return enabled
}

// RequireFeature blocks (403) requests whose tenant has `key` disabled. Platform
// admins always pass (they operate across tenants). Attach to a module's routes.
func (h *Handler) RequireFeature(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, ok := auth.FromContext(r.Context()); ok && auth.AtLeast(c.Role, auth.RolePlatformAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			if !h.Enabled(r.Context(), auth.TenantID(r.Context()), key) {
				httpx.Error(w, http.StatusForbidden, "feature not enabled for this account")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// featureState is one registry entry plus whether it's on for the tenant.
type featureState struct {
	Def
	Enabled bool `json:"enabled"`
}

// getForTenant: the full registry with each feature's effective state for {id}.
func (h *Handler) getForTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	ov, err := h.overrides(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read features failed")
		return
	}
	eff := effective(ov)
	out := make([]featureState, 0, len(Registry))
	for _, d := range Registry {
		out = append(out, featureState{Def: d, Enabled: eff[d.Key]})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// setForTenant replaces the tenant's entitlements from a {key: bool} map. Only
// known keys are stored; a key set to its default (true) is removed to keep the
// table sparse (absence = enabled).
func (h *Handler) setForTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid tenant id")
		return
	}
	var req map[string]bool
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	ctx := r.Context()
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "save failed")
		return
	}
	defer tx.Rollback(ctx)
	for _, d := range Registry {
		enabled, present := req[d.Key]
		if !present {
			continue
		}
		if enabled {
			// default state → drop any override
			if _, err := tx.Exec(ctx, `DELETE FROM tenant_features WHERE tenant_id = $1 AND feature_key = $2`, id, d.Key); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "save failed")
				return
			}
		} else {
			if _, err := tx.Exec(ctx, `
INSERT INTO tenant_features (tenant_id, feature_key, enabled, updated_at)
VALUES ($1, $2, false, now())
ON CONFLICT (tenant_id, feature_key) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = now()`, id, d.Key); err != nil {
				httpx.Error(w, http.StatusInternalServerError, "save failed")
				return
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "save failed")
		return
	}
	h.getForTenant(w, r)
}

// getMine: the caller's own tenant effective features as {key: bool} — the app
// uses this to hide disabled modules.
func (h *Handler) getMine(w http.ResponseWriter, r *http.Request) {
	ov, err := h.overrides(r.Context(), auth.TenantID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read features failed")
		return
	}
	httpx.JSON(w, http.StatusOK, effective(ov))
}
