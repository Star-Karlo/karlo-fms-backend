// Package platformadmin is the SERVICE-operator oversight console: read-only
// views ACROSS every tenant, restricted to RolePlatformAdmin (the operator of
// the platform, not any customer admin).
//
// HOW IT STAYS SAFE UNDER RLS. `vehicles` has a STRICT tenant policy, so there
// is no honest single-query cross-tenant read. Every account is read INSIDE its
// own scope — list the tenants (the `tenants` table has no RLS), then loop and
// run each account's query under db.Tx(tenantID). db.SystemTx must NOT be used
// on tenant tables: it clears the scope and the login-bootstrap policies would
// then expose every tenant at once. N tenants = N round trips, which is cheap
// and keeps isolation exactly where the rest of the app enforces it. This is the
// same discipline as internal/tenants.
package platformadmin

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdevents"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
)

type Handler struct {
	pool   *db.Pool
	live   *liveview.Service
	md     *mdwrite.Client         // nil before cutover; the backfill needs it
	dir    mdwrite.CompanyResolver // tenant id -> company, for the backfill's acting-for
	wakers []mdevents.Waker        // see WithSyncWakers
	authUp *auth.Upstream          // nil before cutover; renames then stay local
}

func NewHandler(pool *db.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

// WithMasterData enables the backfill, which pushes FMS-only rows upstream as
// the operator who triggers it.
func (h *Handler) WithMasterData(md *mdwrite.Client) *Handler { h.md = md; return h }

// WithResolver supplies the tenant -> company lookup the backfill needs.
func (h *Handler) WithResolver(dir mdwrite.CompanyResolver) *Handler { h.dir = dir; return h }

// WithAuthUpstream routes an account rename to the authentication service,
// which owns companies, as the calling operator.
func (h *Handler) WithAuthUpstream(up *auth.Upstream) *Handler { h.authUp = up; return h }

// WithSyncWakers supplies the master-data projections (vehiclesync,
// driversync) that POST /v1/platform/sync refreshes on demand.
func (h *Handler) WithSyncWakers(w ...mdevents.Waker) *Handler { h.wakers = w; return h }

// Routes mounts the platform console — every endpoint requires platform_admin.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RolePlatformAdmin))
		ar.Get("/v1/platform/overview", h.overview)
		ar.Get("/v1/platform/vehicles", h.vehicles)
		ar.Get("/v1/platform/users", h.users)
		ar.Put("/v1/platform/users/{id}/password", h.resetPassword)
		ar.Put("/v1/platform/tenants/{id}/name", h.renameTenant)
		ar.Post("/v1/platform/sync", h.sync)
		ar.Get("/v1/platform/trackers/export", h.trackersExport)
		h.backfillRoutes(ar)
	})
}

// sync godoc
//
//	@Summary		Refresh the master-data projections now (platform admin)
//	@Description	Asks vehiclesync and driversync to pull every company's fleet and drivers from master data immediately instead of at their next tick. The pass runs in the background; the response says only that it was requested. Master-data change notices on the platform Redis do the same automatically.
//	@Tags			platform
//	@Security		BearerAuth
//	@Success		202	{object}	map[string]any
//	@Failure		503	{object}	map[string]string	"no projection configured (pre-cutover deployment)"
//	@Router			/v1/platform/sync [post]
func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	if len(h.wakers) == 0 {
		httpx.Error(w, http.StatusServiceUnavailable, "master-data projection is not configured")
		return
	}
	for _, wk := range h.wakers {
		wk.Wake()
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"requested": true, "projections": len(h.wakers)})
}

// renameTenant godoc
//
//	@Summary		Rename a customer account (platform admin)
//	@Description	Change an account's display name. Companies belong to the authentication service: the rename is sent there as the calling operator (PUT /api/v1/companies/{id}), and the local copy follows — immediately here, and again from the company notice tenantsync receives. platform_admin only.
//	@Tags			platform
//	@Security		BearerAuth
//	@Accept			json
//	@Param			id		path	int		true	"tenant id"
//	@Param			body	body	object	true	"{\"name\":\"...\"}"
//	@Success		204
//	@Router			/v1/platform/tenants/{id}/name [put]
func (h *Handler) renameTenant(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	name := strings.TrimSpace(body.Name)
	// Upstream first: a rename that only lands here is undone by the next
	// tenantsync pass, which is exactly what used to happen.
	if h.authUp != nil {
		company := ""
		if h.dir != nil {
			company, _ = h.dir.CompanyForTenant(r.Context(), id)
		}
		if company == "" {
			httpx.Error(w, http.StatusConflict, "this account is not paired with a company in the authentication service; rename it there")
			return
		}
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		status, raw, err := h.authUp.Forward(r.Context(), http.MethodPut, "/api/v1/companies/"+company, bearer, map[string]any{"name": name})
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		if status < 200 || status >= 300 {
			httpx.Error(w, http.StatusBadGateway, "authentication service refused the rename: "+strings.TrimSpace(string(raw)))
			return
		}
	}
	tag, err := h.pool.Exec(r.Context(), `UPDATE tenants SET name = $2, updated_at = now() WHERE id = $1`, id, name)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "rename failed")
		return
	}
	if tag.RowsAffected() == 0 {
		httpx.Error(w, http.StatusNotFound, "account not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tenantRef struct {
	id   int64
	name string
}

// tenantList reads all tenants (no RLS on `tenants`).
func (h *Handler) tenantList(ctx context.Context) ([]tenantRef, error) {
	rows, err := h.pool.Query(ctx, `SELECT id, name FROM tenants ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tenantRef
	for rows.Next() {
		var t tenantRef
		if err := rows.Scan(&t.id, &t.name); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// PlatformVehicle is one vehicle in the cross-tenant list, tagged with its owner.
type PlatformVehicle struct {
	TenantID     int64    `json:"tenant_id"`
	TenantName   string   `json:"tenant_name"`
	VehicleID    int64    `json:"vehicle_id"`
	LicensePlate string   `json:"license_plate"`
	VehicleType  *string  `json:"vehicle_type"`
	Online       bool     `json:"online"`
	DriveState   string   `json:"drive_state"`
	StaleMinutes *float64 `json:"stale_minutes"`
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	Speed        *float64 `json:"speed"`
	LastSeen     string   `json:"last_seen,omitempty"`
	Location     string   `json:"location,omitempty"`
}

// gather returns every vehicle across the given tenants, each read inside its own
// tenant scope. onlyTenant > 0 restricts to a single account (drill-in).
func (h *Handler) gather(ctx context.Context, tenants []tenantRef, onlyTenant int64) []PlatformVehicle {
	out := []PlatformVehicle{}
	for _, t := range tenants {
		if onlyTenant > 0 && t.id != onlyTenant {
			continue
		}
		vs, err := h.live.List(ctx, t.id, nil)
		if err != nil {
			continue // one unreadable account shouldn't blank the whole console
		}
		for _, v := range vs {
			pv := PlatformVehicle{
				TenantID: t.id, TenantName: t.name,
				VehicleID: v.VehicleID, LicensePlate: v.LicensePlate, VehicleType: v.VehicleType,
				Online: v.Online, DriveState: v.DriveState, StaleMinutes: v.StaleMinutes,
			}
			if v.Position != nil {
				pv.Lat, pv.Lon, pv.Speed = v.Position.Lat, v.Position.Lon, v.Position.Speed
				if !v.Position.Time.IsZero() {
					pv.LastSeen = v.Position.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
				}
				pv.Location = joinLoc(v.Position.Kota, v.Position.Provinsi)
			}
			out = append(out, pv)
		}
	}
	return out
}

func joinLoc(kota, prov string) string {
	switch {
	case kota != "" && prov != "":
		return kota + ", " + prov
	case kota != "":
		return kota
	default:
		return prov
	}
}

// vehicles godoc
//
//	@Summary		All vehicles across every account (platform admin)
//	@Description	Read-only cross-tenant vehicle list with live status, each tagged by owning account. Optional tenant_id drills into one account. Restricted to platform_admin.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Param			tenant_id	query		int	false	"restrict to one account"
//	@Success		200	{object}	httpx.List[platformadmin.PlatformVehicle]
//	@Failure		403	{object}	map[string]string
//	@Router			/v1/platform/vehicles [get]
func (h *Handler) vehicles(w http.ResponseWriter, r *http.Request) {
	var only int64
	if v := r.URL.Query().Get("tenant_id"); v != "" {
		only, _ = strconv.ParseInt(v, 10, 64)
	}
	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	items := h.gather(r.Context(), tenants, only)
	httpx.JSON(w, http.StatusOK, httpx.List[PlatformVehicle]{Items: items, Total: int64(len(items))})
}

// TenantStat is per-account rollup for the overview.
type TenantStat struct {
	TenantID int64  `json:"tenant_id"`
	Name     string `json:"name"`
	Vehicles int    `json:"vehicles"`
	Online   int    `json:"online"`
}

// Overview is the platform-wide summary.
type Overview struct {
	Tenants   int          `json:"tenants"`
	Vehicles  int          `json:"vehicles"`
	Online    int          `json:"online"`
	PerTenant []TenantStat `json:"per_tenant"`
}

// overview godoc
//
//	@Summary		Platform-wide overview (platform admin)
//	@Description	Totals across every account: accounts, vehicles, online, and a per-account breakdown. Restricted to platform_admin.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	platformadmin.Overview
//	@Failure		403	{object}	map[string]string
//	@Router			/v1/platform/overview [get]
func (h *Handler) overview(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "overview failed")
		return
	}
	all := h.gather(r.Context(), tenants, 0)

	byTenant := map[int64]*TenantStat{}
	for _, t := range tenants {
		byTenant[t.id] = &TenantStat{TenantID: t.id, Name: t.name}
	}
	online := 0
	for _, v := range all {
		s := byTenant[v.TenantID]
		if s == nil {
			continue
		}
		s.Vehicles++
		if v.Online {
			s.Online++
			online++
		}
	}
	per := make([]TenantStat, 0, len(tenants))
	for _, t := range tenants {
		per = append(per, *byTenant[t.id])
	}
	httpx.JSON(w, http.StatusOK, Overview{Tenants: len(tenants), Vehicles: len(all), Online: online, PerTenant: per})
}

// PlatformUser is one login account, tagged with its owning tenant.
type PlatformUser struct {
	ID          int64   `json:"id"`
	TenantID    int64   `json:"tenant_id"`
	TenantName  string  `json:"tenant_name"`
	Email       string  `json:"email"`
	FullName    *string `json:"full_name"`
	Role        string  `json:"role"`
	Status      string  `json:"status"`
	LastLoginAt *string `json:"last_login_at"`
}

// users godoc
//
//	@Summary		All login accounts across every tenant (platform admin)
//	@Description	Cross-tenant user list — id, account, email, role, status, last login. Read-only, platform_admin only. Also the recovery/diagnostic view (exact stored email + status).
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	httpx.List[platformadmin.PlatformUser]
//	@Router			/v1/platform/users [get]
func (h *Handler) users(w http.ResponseWriter, r *http.Request) {
	out := []PlatformUser{}
	// users has the permissive login-bootstrap RLS policy, so an unset-tenant
	// (SystemTx) read returns every account — exactly what a platform view needs.
	err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT u.id, u.tenant_id, t.name, u.email, u.full_name, u.role, u.status,
       to_char(u.last_login_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00')
FROM users u JOIN tenants t ON t.id = u.tenant_id
ORDER BY u.tenant_id, u.email`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u PlatformUser
			if err := rows.Scan(&u.ID, &u.TenantID, &u.TenantName, &u.Email, &u.FullName, &u.Role, &u.Status, &u.LastLoginAt); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list users failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[PlatformUser]{Items: out, Total: int64(len(out))})
}

// resetPassword godoc
//
//	@Summary		Reset any user's password in any account (platform admin)
//	@Description	Set a new login password for a user in ANY tenant and reactivate the account. The one cross-tenant write besides tenant provisioning — restricted to platform_admin, used to recover a locked-out customer admin.
//	@Tags			platform
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path	int		true	"user id"
//	@Param			body	body	object	true	"{\"password\":\"...\"}"
//	@Success		204
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/platform/users/{id}/password [put]
func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil || len(body.Password) < 8 {
		httpx.Error(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	// Resolve the user's tenant first (SystemTx), then do the write INSIDE that
	// tenant's scope so RLS still governs it — no unscoped write to users.
	var tid int64
	if err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT tenant_id FROM users WHERE id = $1`, id).Scan(&tid)
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "user not found")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), 12)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}
	if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), `UPDATE users SET password_hash = $2, status = 'active', updated_at = now() WHERE id = $1`, id, string(hash))
		return e
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "reset failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
