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

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

type Handler struct {
	pool *db.Pool
	live *liveview.Service
}

func NewHandler(pool *db.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

// Routes mounts the platform console — every endpoint requires platform_admin.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RolePlatformAdmin))
		ar.Get("/v1/platform/overview", h.overview)
		ar.Get("/v1/platform/vehicles", h.vehicles)
	})
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
