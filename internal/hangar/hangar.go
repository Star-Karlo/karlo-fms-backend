// Package hangar manages maintenance hangars — the places a fleet services its
// vehicles — and answers which vehicles are currently in for work.
//
// The occupancy question is the interesting half. "Which trucks are under
// maintenance" has two different answers and they disagree constantly:
//
//	BOOKED   — a service schedule says this vehicle is due at this hangar
//	PRESENT  — the vehicle's last position is inside the hangar's radius
//
// A truck can be booked and not yet arrived, arrived without a booking, or
// booked at one hangar while sitting at another. Collapsing those into a single
// "in maintenance" flag would quietly invent a fact, so both are reported and
// the difference is what makes the view worth looking at: booked-and-absent is
// a no-show, present-and-unbooked is unplanned work nobody scheduled.
package hangar

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/routing"
	"github.com/karlo/fms-backend/internal/site"
)

// Hangar is one maintenance facility.
type Hangar struct {
	ID       int64   `json:"id"`
	Name     string  `json:"name"`
	Address  *string `json:"address"`
	Phone    *string `json:"phone"`
	Notes    *string `json:"notes"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	RadiusM  int     `json:"radius_m"`
	Capacity *int    `json:"capacity"`
	Active   bool    `json:"active"`
	// Booked counts open service schedules pointing here; Present counts
	// vehicles physically inside the radius right now. See the package comment:
	// these are different numbers and the gap between them is the signal.
	Booked  int `json:"booked"`
	Present int `json:"present"`
}

// UpsertRequest is the create/update body.
type UpsertRequest struct {
	Name     string  `json:"name"`
	Address  *string `json:"address"`
	Phone    *string `json:"phone"`
	Notes    *string `json:"notes"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	RadiusM  *int    `json:"radius_m"`
	Capacity *int    `json:"capacity"`
	Active   *bool   `json:"active"`
}

// Handler serves hangar endpoints.
type Handler struct {
	pool   *pgxpool.Pool
	live   *liveview.Service
	router routing.Router
}

func NewHandler(pool *pgxpool.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

// Routes mounts the hangar endpoints.
//
// Reading is open to any authenticated user — an operator needs to see where a
// truck is being serviced. Creating and moving one is admin-only: a hangar is
// part of the company's setup, not a per-user preference.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/hangars", h.list)
	r.Get("/v1/hangars/{id}/vehicles", h.vehicles)
	r.Get("/v1/hangars/{id}/route", h.routeToHangar)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Post("/v1/hangars", h.create)
		ar.Put("/v1/hangars/{id}", h.update)
		ar.Delete("/v1/hangars/{id}", h.remove)
	})
}

const cols = `id, name, address, phone, notes, lat, lon, radius_m, capacity, active`

func scan(row pgx.Row) (*Hangar, error) {
	var g Hangar
	if err := row.Scan(&g.ID, &g.Name, &g.Address, &g.Phone, &g.Notes,
		&g.Lat, &g.Lon, &g.RadiusM, &g.Capacity, &g.Active); err != nil {
		return nil, err
	}
	return &g, nil
}

func validate(req *UpsertRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if req.Lat < -11 || req.Lat > 6 || req.Lon < 95 || req.Lon > 141 {
		return fmt.Errorf("location is outside Indonesia; check that lat and lon are not swapped")
	}
	if req.RadiusM != nil && (*req.RadiusM < 25 || *req.RadiusM > 5000) {
		return fmt.Errorf("radius must be between 25 and 5000 metres")
	}
	if req.Capacity != nil && *req.Capacity <= 0 {
		return fmt.Errorf("capacity must be a positive number, or left empty")
	}
	return nil
}

// list godoc
//
//	@Summary		Maintenance hangars, with how many vehicles are booked and present
//	@Description	`booked` counts open service schedules assigned here; `present` counts vehicles whose last position is inside the radius. They are deliberately separate — booked-but-absent is a no-show, present-but-unbooked is unplanned work.
//	@Tags			hangars
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}	hangar.Hangar
//	@Router			/v1/hangars [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantID(r.Context())
	out := []Hangar{}
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `SELECT `+cols+` FROM hangars ORDER BY active DESC, name`)
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
		// Booked counts, in one pass rather than per hangar.
		brows, err := tx.Query(r.Context(), `
SELECT hangar_id, count(*) FROM service_schedules
WHERE hangar_id IS NOT NULL AND coalesce(status,'') <> 'done'
GROUP BY hangar_id`)
		if err != nil {
			return err
		}
		defer brows.Close()
		booked := map[int64]int{}
		for brows.Next() {
			var id int64
			var n int
			if err := brows.Scan(&id, &n); err != nil {
				return err
			}
			booked[id] = n
		}
		if err := brows.Err(); err != nil {
			return err
		}
		// Presence is read from the recorded visits rather than swept from
		// live positions on every request: the collector already knows who is
		// here, and only it knows since when.
		present, err := site.PresentCounts(r.Context(), tx, site.Hangar)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Booked = booked[out[i].ID]
			out[i].Present = present[out[i].ID]
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	httpx.JSON(w, http.StatusOK, out)
}

// VehicleAtHangar is one vehicle's relationship to a hangar.
type VehicleAtHangar struct {
	VehicleID    int64   `json:"vehicle_id"`
	LicensePlate string  `json:"license_plate"`
	Component    *string `json:"component"`
	DueAt        *string `json:"due_at"`
	Status       *string `json:"status"`
	// Present is whether the vehicle is physically here right now, which is not
	// the same as being booked here.
	Present bool `json:"present"`
	// Since and DwellMin are the arrival time and how long it has been here —
	// the difference between "it is at the workshop" and "it has been at the
	// workshop for three days".
	Since    *time.Time `json:"since,omitempty"`
	DwellMin *float64   `json:"dwell_min,omitempty"`
	// DistanceM is how far away it is, so a no-show can be chased. Only set
	// for vehicles that have NOT arrived.
	DistanceM *int `json:"distance_m,omitempty"`
}

// vehicles godoc
//
//	@Summary	Vehicles booked at, or currently sitting in, one hangar
//	@Tags		hangars
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path	int	true	"hangar id"
//	@Success	200	{array}	hangar.VehicleAtHangar
//	@Router		/v1/hangars/{id}/vehicles [get]
func (h *Handler) vehicles(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	tenantID := auth.TenantID(r.Context())

	var g *Hangar
	out := []VehicleAtHangar{}
	err = db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		g, e = scan(tx.QueryRow(r.Context(), `SELECT `+cols+` FROM hangars WHERE id = $1`, id))
		if e != nil {
			return e
		}
		rows, e := tx.Query(r.Context(), `
SELECT s.vehicle_id, coalesce(v.license_plate,''), s.component,
       to_char(s.next_due_at, 'YYYY-MM-DD'), s.status
FROM service_schedules s
LEFT JOIN vehicles v ON v.id = s.vehicle_id
WHERE s.hangar_id = $1 AND coalesce(s.status,'') <> 'done'
ORDER BY s.next_due_at NULLS LAST`, id)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var a VehicleAtHangar
			if e := rows.Scan(&a.VehicleID, &a.LicensePlate, &a.Component, &a.DueAt, &a.Status); e != nil {
				return e
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "hangar not found")
		return
	}

	// Who is actually here, and since when. The recorded visits answer both;
	// the live sweep below then adds how far away the no-shows are, which
	// visits cannot know.
	var here []site.Visit
	_ = db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		here, e = site.Present(r.Context(), tx, site.Hangar, id)
		return e
	})
	{
		byVehicle := map[int64]site.Visit{}
		for _, v := range here {
			byVehicle[v.VehicleID] = v
		}
		booked := map[int64]int{}
		for i, a := range out {
			booked[a.VehicleID] = i
		}
		for vid, v := range byVehicle {
			since, dwell := v.EnteredAt, v.DwellMin
			if i, ok := booked[vid]; ok {
				out[i].Present = true
				out[i].Since = &since
				out[i].DwellMin = &dwell
				continue
			}
			out = append(out, VehicleAtHangar{
				VehicleID: vid, LicensePlate: v.LicensePlate,
				Present: true, Since: &since, DwellMin: &dwell,
			})
		}
	}

	// Distance for the vehicles that are NOT here: a no-show is chased by
	// knowing how far off it is.
	if h.live != nil {
		if vs, e := h.live.List(r.Context(), tenantID, nil); e == nil {
			booked := map[int64]int{}
			for i, a := range out {
				booked[a.VehicleID] = i
			}
			for _, v := range vs {
				if v.Position == nil || v.Position.Lat == nil || v.Position.Lon == nil {
					continue
				}
				i, ok := booked[v.VehicleID]
				if !ok || out[i].Present {
					continue
				}
				m := int(metersBetween(*v.Position.Lat, *v.Position.Lon, g.Lat, g.Lon))
				out[i].DistanceM = &m
			}
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// InMaintenance is a vehicle with open service work.
type InMaintenance struct {
	VehicleID    int64   `json:"vehicle_id"`
	LicensePlate string  `json:"license_plate"`
	Component    *string `json:"component"`
	DueAt        *string `json:"due_at"`
	Status       string  `json:"status"`
	HangarID     *int64  `json:"hangar_id"`
	HangarName   string  `json:"hangar_name,omitempty"`
	Present      bool    `json:"present"`
	DistanceM    *int    `json:"distance_m,omitempty"`

	// Unexported: needed to compute Present, not worth exposing.
	hangarLat, hangarLon *float64
	hangarRadius         *int
}

// create godoc
//
//	@Summary	Add a maintenance hangar (admin only)
//	@Tags		hangars
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		hangar.UpsertRequest	true	"hangar"
//	@Success	201		{object}	hangar.Hangar
//	@Failure	400		{object}	map[string]string
//	@Router		/v1/hangars [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req UpsertRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := validate(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	tenantID := auth.TenantID(r.Context())
	var out *Hangar
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		out, e = scan(tx.QueryRow(r.Context(), `
INSERT INTO hangars (tenant_id, name, address, phone, notes, lat, lon, radius_m, capacity, active)
VALUES ($1,$2,$3,$4,$5,$6,$7,coalesce($8,150),$9,coalesce($10,true))
RETURNING `+cols,
			tenantID, strings.TrimSpace(req.Name), req.Address, req.Phone, req.Notes,
			req.Lat, req.Lon, req.RadiusM, req.Capacity, req.Active))
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, friendly(err))
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// update godoc
//
//	@Summary	Update a hangar (admin only)
//	@Tags		hangars
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int						true	"hangar id"
//	@Param		body	body		hangar.UpsertRequest	true	"hangar"
//	@Success	200		{object}	hangar.Hangar
//	@Router		/v1/hangars/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req UpsertRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if err := validate(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var out *Hangar
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		var moved bool
		if err := tx.QueryRow(r.Context(),
			`SELECT abs(lat-$2) > 0.00001 OR abs(lon-$3) > 0.00001 OR radius_m <> coalesce($4, radius_m)
			   FROM hangars WHERE id = $1`, id, req.Lat, req.Lon, req.RadiusM).Scan(&moved); err != nil {
			return err
		}
		var e error
		out, e = scan(tx.QueryRow(r.Context(), `
UPDATE hangars SET name=$2, address=$3, phone=$4, notes=$5, lat=$6, lon=$7,
  radius_m=coalesce($8,radius_m), capacity=$9, active=coalesce($10,active), updated_at=now()
WHERE id=$1
RETURNING `+cols,
			id, strings.TrimSpace(req.Name), req.Address, req.Phone, req.Notes,
			req.Lat, req.Lon, req.RadiusM, req.Capacity, req.Active))
		if e != nil {
			return e
		}
		// Moving the hangar ends the stays recorded at where it used to be;
		// whoever is inside the new circle starts a fresh one next pass.
		if moved || !out.Active {
			return site.CloseAllFor(r.Context(), tx, site.Hangar, id)
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, friendly(err))
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// remove godoc
//
//	@Summary		Delete a hangar (admin only)
//	@Description	Service schedules pointing here are kept and their hangar is cleared, rather than the delete being blocked — losing the maintenance history to tidy up a location would be the worse trade.
//	@Tags			hangars
//	@Security		BearerAuth
//	@Param			id	path	int	true	"hangar id"
//	@Success		204
//	@Router			/v1/hangars/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), `DELETE FROM hangars WHERE id = $1`, id)
		return e
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// friendly turns the one constraint a user can realistically trip into plain
// language; everything else is passed through.
func friendly(err error) string {
	s := err.Error()
	if strings.Contains(s, "hangars_tenant_id_name_key") {
		return "a hangar with that name already exists"
	}
	return s
}

// metersBetween is the haversine distance between two WGS84 points.
//
// Done in Go rather than PostGIS on purpose: the vehicle positions come from
// the telemetry service over HTTP and are never in the database, so a spatial
// query would mean shipping every live position into Postgres just to measure
// it against a handful of hangars.
func metersBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const earthR = 6371000.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat, dLon := rad(lat2-lat1), rad(lon2-lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthR * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// ---------------------------------------------------------------------------
// Routing a vehicle to its hangar
// ---------------------------------------------------------------------------

// WithRouter attaches a routing provider. Optional: without one the route
// endpoint answers 503 and every other hangar endpoint keeps working.
func (h *Handler) WithRouter(r routing.Router) *Handler {
	h.router = r
	return h
}

// RouteToHangar is a drivable path from where a vehicle is now to a hangar.
type RouteToHangar struct {
	VehicleID    int64   `json:"vehicle_id"`
	LicensePlate string  `json:"license_plate"`
	HangarID     int64   `json:"hangar_id"`
	HangarName   string  `json:"hangar_name"`
	DistanceKm   float64 `json:"distance_km"`
	// DurationMin is the provider's free-flow estimate. Named plainly rather
	// than "ETA" because it contains no traffic, no loading time and no driver
	// break — calling it an ETA would promise something it cannot know.
	DurationMin float64      `json:"duration_min"`
	Geometry    [][2]float64 `json:"geometry"`
	Provider    string       `json:"provider"`
	// AlreadyThere is true when the vehicle is inside the hangar radius, in
	// which case no route was requested at all.
	AlreadyThere bool `json:"already_there"`
}

// routeToHangar godoc
//
//	@Summary		Drivable route from a vehicle's current position to a hangar
//	@Description	Planned on the heavy-goods profile, so weight and height restrictions are respected — a car route that sends a 40ft trailer down a residential shortcut is worse than none. `duration_min` is a free-flow estimate with no traffic, loading or break time in it. Returns `already_there` without a route when the vehicle is inside the hangar radius.
//	@Tags			hangars
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id			path		int	true	"hangar id"
//	@Param			vehicle_id	query		int	true	"vehicle id"
//	@Success		200			{object}	hangar.RouteToHangar
//	@Failure		400			{object}	map[string]string
//	@Failure		503			{object}	map[string]string
//	@Router			/v1/hangars/{id}/route [get]
func (h *Handler) routeToHangar(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"routing is not configured on this deployment")
		return
	}
	hangarID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid hangar id")
		return
	}
	vehicleID, err := strconv.ParseInt(r.URL.Query().Get("vehicle_id"), 10, 64)
	if err != nil || vehicleID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "vehicle_id is required")
		return
	}
	tenantID := auth.TenantID(r.Context())

	var g *Hangar
	if err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		g, e = scan(tx.QueryRow(r.Context(), `SELECT `+cols+` FROM hangars WHERE id = $1`, hangarID))
		return e
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "hangar not found")
		return
	}

	if h.live == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "live positions are unavailable")
		return
	}
	vehicles, err := h.live.List(r.Context(), tenantID, nil)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	var veh *liveview.VehicleLive
	for i := range vehicles {
		if vehicles[i].VehicleID == vehicleID {
			veh = &vehicles[i]
			break
		}
	}
	if veh == nil {
		httpx.Error(w, http.StatusNotFound, "vehicle not found")
		return
	}
	if veh.Position == nil || veh.Position.Lat == nil || veh.Position.Lon == nil {
		httpx.Error(w, http.StatusBadRequest,
			"this vehicle has no recent position, so a route cannot be planned from it")
		return
	}

	out := RouteToHangar{
		VehicleID: vehicleID, LicensePlate: veh.LicensePlate,
		HangarID: g.ID, HangarName: g.Name,
	}

	// Already inside the radius: skip the provider entirely. Routing a truck to
	// where it is standing burns quota to draw a dot.
	if metersBetween(*veh.Position.Lat, *veh.Position.Lon, g.Lat, g.Lon) <= float64(g.RadiusM) {
		out.AlreadyThere = true
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	route, err := h.router.Route(r.Context(),
		routing.Point{Lat: *veh.Position.Lat, Lon: *veh.Position.Lon},
		routing.Point{Lat: g.Lat, Lon: g.Lon})
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	out.DistanceKm = float64(int(route.DistanceM/100+0.5)) / 10
	out.DurationMin = float64(int(route.DurationS/6+0.5)) / 10
	out.Geometry = route.Geometry
	out.Provider = route.Provider
	httpx.JSON(w, http.StatusOK, out)
}
