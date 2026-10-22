// Package geofence is the places a fleet watches: named circles that record
// which vehicles are inside them, since when, and for how long.
//
// A geofence and a hangar are deliberately the same shape (see internal/site).
// The difference is what you do with one: a hangar is somewhere you SEND a
// truck — it has a route endpoint and service bookings — while a geofence is
// somewhere you WATCH, and the only thing you do to it is wait.
//
// Visits are recorded by the telemetry collector for every geofence, whether
// or not anybody subscribed to alerts, so presence and the movement report are
// always answerable. Attaching vehicles (geofence_vehicles) or creating a
// notification rule is how you ask to be TOLD; it is not how recording starts.
package geofence

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/site"
)

// Geofence is a named circle.
type Geofence struct {
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Address *string `json:"address"`
	Notes   *string `json:"notes"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m"`
	// MinDwellMin is how long a vehicle must stay inside before it counts as
	// having arrived. 0 means the first fix inside counts, which is right for
	// a large site and wrong for a small one on a through road.
	MinDwellMin int       `json:"min_dwell_min"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	// Present is how many vehicles are inside right now.
	Present int `json:"present"`
	// Watching is how many vehicles are attached; 0 means the whole fleet.
	Watching int `json:"watching"`
}

// UpsertRequest is the create/update body.
type UpsertRequest struct {
	Name        string  `json:"name"`
	Address     *string `json:"address"`
	Notes       *string `json:"notes"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	RadiusM     float64 `json:"radius_m"`
	MinDwellMin *int    `json:"min_dwell_min"`
	Active      *bool   `json:"active"`
}

// CreateRequest is kept as the documented name for the create body.
type CreateRequest = UpsertRequest

// Handler exposes geofence endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts geofence endpoints (inside the authenticated group).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/geofences", h.list)
	r.Post("/v1/geofences", h.create)
	r.Get("/v1/geofences/{id}", h.get)
	r.Put("/v1/geofences/{id}", h.update)
	r.Delete("/v1/geofences/{id}", h.remove)
	r.Get("/v1/geofences/{id}/presence", h.presence)
	r.Get("/v1/geofences/visits", h.visits)
	r.Get("/v1/geofences/{id}/vehicles", h.listVehicles)
	r.Post("/v1/geofences/{id}/vehicles/{vehicleID}", h.attachVehicle)
	r.Delete("/v1/geofences/{id}/vehicles/{vehicleID}", h.detachVehicle)
}

const cols = `id, name, address, notes,
	ST_Y(center::geometry)::float8, ST_X(center::geometry)::float8,
	COALESCE(radius_m, 0)::float8, min_dwell_min, active, created_at`

func scan(row pgx.Row) (*Geofence, error) {
	var g Geofence
	if err := row.Scan(&g.ID, &g.Name, &g.Address, &g.Notes, &g.Lat, &g.Lon,
		&g.RadiusM, &g.MinDwellMin, &g.Active, &g.CreatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}

func validate(req *UpsertRequest) string {
	if strings.TrimSpace(req.Name) == "" {
		return "name is required"
	}
	if req.RadiusM < 25 || req.RadiusM > 50000 {
		return "radius must be between 25 and 50000 metres"
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 {
		return "lat/lon are out of range — check they are not swapped"
	}
	if req.MinDwellMin != nil && (*req.MinDwellMin < 0 || *req.MinDwellMin > 1440) {
		return "the arrival delay must be between 0 minutes and 24 hours"
	}
	return ""
}

// list godoc
//
//	@Summary		List geofences, with how many vehicles are inside each one
//	@Description	`present` counts vehicles currently inside (confirmed visits — a vehicle that only clipped the edge is not counted). `watching` is how many vehicles are attached for alerts; 0 means the whole fleet.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}	geofence.Geofence
//	@Router			/v1/geofences [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	out := []Geofence{}
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+cols+`
			FROM geofences WHERE center IS NOT NULL ORDER BY active DESC, name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			g, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, *g)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		present, err := site.PresentCounts(r.Context(), tx, site.Geofence)
		if err != nil {
			return err
		}
		watching := map[int64]int{}
		wrows, err := tx.Query(r.Context(), `SELECT geofence_id, count(*) FROM geofence_vehicles GROUP BY 1`)
		if err != nil {
			return err
		}
		defer wrows.Close()
		for wrows.Next() {
			var id int64
			var n int
			if err := wrows.Scan(&id, &n); err != nil {
				return err
			}
			watching[id] = n
		}
		for i := range out {
			out[i].Present = present[out[i].ID]
			out[i].Watching = watching[out[i].ID]
		}
		return wrows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// get godoc
//
//	@Summary	One geofence
//	@Tags		geofences
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"geofence id"
//	@Success	200	{object}	geofence.Geofence
//	@Failure	404	{object}	map[string]string
//	@Router		/v1/geofences/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var g *Geofence
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var e error
		g, e = scan(tx.QueryRow(r.Context(), `SELECT `+cols+` FROM geofences WHERE id = $1`, id))
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "geofence not found")
		return
	}
	httpx.JSON(w, http.StatusOK, g)
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
//	@Failure	400		{object}	map[string]string
//	@Router		/v1/geofences [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if msg := validate(&req); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	tenantID := auth.EffectiveTenant(r)
	var g *Geofence
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		g, e = scan(tx.QueryRow(r.Context(), `
			INSERT INTO geofences (tenant_id, name, address, notes, center, radius_m, min_dwell_min, active)
			VALUES ($1, $2, $3, $4, ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography, $7, $8, $9)
			RETURNING `+cols,
			tenantID, strings.TrimSpace(req.Name), req.Address, req.Notes,
			req.Lon, req.Lat, req.RadiusM, intOr(req.MinDwellMin, 0), boolOr(req.Active, true)))
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, g)
}

// update godoc
//
//	@Summary		Edit a geofence
//	@Description	Moving a geofence or changing its radius ENDS every open visit at it: the stays recorded so far happened at the old circle, and leaving them open would attribute time to a place that is no longer where it was. Vehicles inside the new circle open a fresh visit on the next collector pass.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int						true	"geofence id"
//	@Param			body	body		geofence.UpsertRequest	true	"geofence"
//	@Success		200		{object}	geofence.Geofence
//	@Failure		400		{object}	map[string]string
//	@Failure		404		{object}	map[string]string
//	@Router			/v1/geofences/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req UpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if msg := validate(&req); msg != "" {
		httpx.Error(w, http.StatusBadRequest, msg)
		return
	}
	var g *Geofence
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var moved bool
		if err := tx.QueryRow(r.Context(), `
			SELECT ST_Distance(center, ST_SetSRID(ST_MakePoint($2, $3), 4326)::geography) > 1
			    OR COALESCE(radius_m,0) <> $4
			  FROM geofences WHERE id = $1`, id, req.Lon, req.Lat, req.RadiusM).Scan(&moved); err != nil {
			return err
		}
		var e error
		g, e = scan(tx.QueryRow(r.Context(), `
			UPDATE geofences SET
			  name = $2, address = $3, notes = $4,
			  center = ST_SetSRID(ST_MakePoint($5, $6), 4326)::geography,
			  radius_m = $7, min_dwell_min = $8, active = $9, updated_at = now()
			WHERE id = $1
			RETURNING `+cols,
			id, strings.TrimSpace(req.Name), req.Address, req.Notes,
			req.Lon, req.Lat, req.RadiusM, intOr(req.MinDwellMin, 0), boolOr(req.Active, true)))
		if e != nil {
			return e
		}
		if moved || !g.Active {
			return site.CloseAllFor(r.Context(), tx, site.Geofence, id)
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "geofence not found")
		return
	}
	httpx.JSON(w, http.StatusOK, g)
}

// remove godoc
//
//	@Summary		Delete a geofence
//	@Description	Its recorded visits go with it, so the movement report loses this site's history. Deactivating instead keeps the record and stops the watching.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Param			id	path	int	true	"geofence id"
//	@Success		204
//	@Router			/v1/geofences/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM geofences WHERE id = $1`, id)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// presence godoc
//
//	@Summary		Vehicles inside a geofence right now, and since when
//	@Description	Longest stay first. `dwell_min` counts from the first fix inside, including the part before the arrival delay confirmed it. A vehicle that only clipped the edge never appears.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path	int	true	"geofence id"
//	@Success		200	{array}	site.Visit
//	@Router			/v1/geofences/{id}/presence [get]
func (h *Handler) presence(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var out []site.Visit
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var e error
		out, e = site.Present(r.Context(), tx, site.Geofence, id)
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// visits godoc
//
//	@Summary		Movement log: who entered and left which geofence, in time order
//	@Description	Every visit overlapping the window, oldest arrival first. Omit `geofence_ids` for all of them. A visit still open has a null `exited_at` and a `dwell_min` counted up to now.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Produce		json
//	@Param			from			query	string	true	"RFC3339"
//	@Param			to				query	string	true	"RFC3339"
//	@Param			geofence_ids	query	string	false	"comma-separated geofence ids (default: all)"
//	@Success		200	{array}	site.Visit
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/geofences/visits [get]
func (h *Handler) visits(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}
	var ids []int64
	for _, s := range strings.Split(q.Get("geofence_ids"), ",") {
		if s = strings.TrimSpace(s); s != "" {
			if id, err := strconv.ParseInt(s, 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	var out []site.Visit
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		var e error
		out, e = site.Window(r.Context(), tx, site.Geofence, ids, from, to)
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// AssignedVehicle is a vehicle assigned to a geofence.
type AssignedVehicle struct {
	VehicleID    int64  `json:"vehicle_id"`
	LicensePlate string `json:"license_plate"`
}

// listVehicles godoc
//
//	@Summary		Vehicles attached to a geofence (they get Enter/Exit alerts)
//	@Description	An empty list means the geofence watches every vehicle in the account. Attaching narrows it.
//	@Tags			geofences
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path	int	true	"geofence id"
//	@Success		200	{array}	geofence.AssignedVehicle
//	@Router			/v1/geofences/{id}/vehicles [get]
func (h *Handler) listVehicles(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	out := []AssignedVehicle{}
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
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
//	@Summary	Attach a vehicle to a geofence (starts Enter/Exit alerts for it)
//	@Tags		geofences
//	@Security	BearerAuth
//	@Param		id			path	int	true	"geofence id"
//	@Param		vehicleID	path	int	true	"vehicle id"
//	@Success	204
//	@Router		/v1/geofences/{id}/vehicles/{vehicleID} [post]
func (h *Handler) attachVehicle(w http.ResponseWriter, r *http.Request) {
	id, vid, ok := pathIDs(w, r)
	if !ok {
		return
	}
	tenantID := auth.EffectiveTenant(r)
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
//	@Summary	Detach a vehicle from a geofence
//	@Tags		geofences
//	@Security	BearerAuth
//	@Param		id			path	int	true	"geofence id"
//	@Param		vehicleID	path	int	true	"vehicle id"
//	@Success	204
//	@Router		/v1/geofences/{id}/vehicles/{vehicleID} [delete]
func (h *Handler) detachVehicle(w http.ResponseWriter, r *http.Request) {
	id, vid, ok := pathIDs(w, r)
	if !ok {
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM geofence_vehicles WHERE geofence_id = $1 AND vehicle_id = $2`, id, vid)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func pathIDs(w http.ResponseWriter, r *http.Request) (int64, int64, bool) {
	id, err1 := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	vid, err2 := strconv.ParseInt(chi.URLParam(r, "vehicleID"), 10, 64)
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return 0, 0, false
	}
	return id, vid, true
}

func intOr(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}
