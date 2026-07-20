// Package geofence is CRUD for circle geofences (the geofences table has had a
// PostGIS shape since 0006 but no endpoints until now). The Live View map draws
// them and the telemetrysync collector evaluates enter/exit transitions for
// vehicles attached to GEOFENCE_ENTER/EXIT notification rules.
package geofence

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Geofence is a named circle.
type Geofence struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	RadiusM   float64   `json:"radius_m"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRequest creates a circle geofence.
type CreateRequest struct {
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m"`
}

// Handler exposes geofence endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts geofence endpoints (inside the authenticated group).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/geofences", h.list)
	r.Post("/v1/geofences", h.create)
	r.Delete("/v1/geofences/{id}", h.remove)
	r.Get("/v1/geofences/{id}/vehicles", h.listVehicles)
	r.Post("/v1/geofences/{id}/vehicles/{vehicleID}", h.attachVehicle)
	r.Delete("/v1/geofences/{id}/vehicles/{vehicleID}", h.detachVehicle)
}

// AssignedVehicle is a vehicle assigned to a geofence.
type AssignedVehicle struct {
	VehicleID    int64  `json:"vehicle_id"`
	LicensePlate string `json:"license_plate"`
}

// listVehicles godoc
//
//	@Summary	Vehicles assigned to a geofence (they get Enter/Exit alerts)
//	@Tags		geofences
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path	int	true	"geofence id"
//	@Success	200	{array}	geofence.AssignedVehicle
//	@Router		/v1/geofences/{id}/vehicles [get]
func (h *Handler) listVehicles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	out := []AssignedVehicle{}
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT gv.vehicle_id, v.license_plate
			FROM geofence_vehicles gv JOIN vehicles v ON v.id = gv.vehicle_id
			WHERE gv.geofence_id = $1 ORDER BY v.license_plate`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a AssignedVehicle
			if err := rows.Scan(&a.VehicleID, &a.LicensePlate); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// attachVehicle godoc
//
//	@Summary	Assign a vehicle to a geofence (starts Enter/Exit alerts)
//	@Tags		geofences
//	@Security	BearerAuth
//	@Param		id			path	int	true	"geofence id"
//	@Param		vehicleID	path	int	true	"vehicle id"
//	@Success	204
//	@Router		/v1/geofences/{id}/vehicles/{vehicleID} [post]
func (h *Handler) attachVehicle(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, err2 := strconv.ParseInt(chi.URLParam(r, "vehicleID"), 10, 64)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	tenantID := auth.TenantID(r.Context())
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO geofence_vehicles (tenant_id, geofence_id, vehicle_id)
			VALUES ($1, $2, $3) ON CONFLICT (tenant_id, geofence_id, vehicle_id) DO NOTHING`,
			tenantID, id, vid)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// detachVehicle godoc
//
//	@Summary	Unassign a vehicle from a geofence
//	@Tags		geofences
//	@Security	BearerAuth
//	@Param		id			path	int	true	"geofence id"
//	@Param		vehicleID	path	int	true	"vehicle id"
//	@Success	204
//	@Router		/v1/geofences/{id}/vehicles/{vehicleID} [delete]
func (h *Handler) detachVehicle(w http.ResponseWriter, r *http.Request) {
	id, err1 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, err2 := strconv.ParseInt(chi.URLParam(r, "vehicleID"), 10, 64)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM geofence_vehicles WHERE geofence_id = $1 AND vehicle_id = $2`, id, vid)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// list godoc
//
//	@Summary	List geofences
//	@Tags		geofences
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	geofence.Geofence
//	@Router		/v1/geofences [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out := []Geofence{}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
			SELECT id, name, ST_Y(center::geometry)::float8, ST_X(center::geometry)::float8,
			       COALESCE(radius_m, 0)::float8, created_at
			FROM geofences WHERE center IS NOT NULL ORDER BY id DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g Geofence
			if err := rows.Scan(&g.ID, &g.Name, &g.Lat, &g.Lon, &g.RadiusM, &g.CreatedAt); err != nil {
				return err
			}
			out = append(out, g)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// create godoc
//
//	@Summary	Create a circle geofence
//	@Tags		geofences
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		geofence.CreateRequest	true	"geofence"
//	@Success	201		{object}	geofence.Geofence
//	@Router		/v1/geofences [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" ||
		req.RadiusM <= 0 || req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		httpx.Error(w, http.StatusBadRequest, "name, lat, lon and a positive radius_m are required")
		return
	}
	tenantID := auth.TenantID(r.Context())
	var g Geofence
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			INSERT INTO geofences (tenant_id, name, center, radius_m)
			VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326)::geography, $5)
			RETURNING id, name, ST_Y(center::geometry)::float8, ST_X(center::geometry)::float8,
			          radius_m::float8, created_at`,
			tenantID, req.Name, req.Lon, req.Lat, req.RadiusM).
			Scan(&g.ID, &g.Name, &g.Lat, &g.Lon, &g.RadiusM, &g.CreatedAt)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, g)
}

// remove godoc
//
//	@Summary	Delete a geofence
//	@Tags		geofences
//	@Security	BearerAuth
//	@Param		id	path	int	true	"geofence id"
//	@Success	204
//	@Router		/v1/geofences/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM geofences WHERE id = $1`, id)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
