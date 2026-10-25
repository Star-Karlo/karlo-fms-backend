package maintenance

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/routing"
	"github.com/karlo/fms-backend/internal/store"
)

// Dispatch sends a vehicle to a hangar for a service that is due, and marks it
// off the road while it is there.
//
// WHAT THIS JOINS UP. The pieces existed separately: a schedule knew the
// service was due, a hangar knew where the workshop was and could route to it,
// site_visits already noticed when a truck arrived, and vehicles.status had a
// 'repair' value nothing ever set. Dispatching is the act that connects them —
// one decision, recorded once, rather than four things a person has to
// remember to do consistently.
//
// THE ROUTE IS COMPUTED SERVER-SIDE, not passed in by the caller. The distance
// at dispatch is operational evidence — how far the truck was when it was sent
// — so it must not be whatever the browser felt like reporting.

// DispatchRequest books the work into a hangar.
type DispatchRequest struct {
	// HangarID is required here even though the column is nullable, because a
	// dispatch with no destination is not a dispatch. A schedule can still be
	// created, and serviced roadside, without one.
	HangarID int64 `json:"hangar_id"`
}

// DispatchResult is the schedule's new state plus the journey to the workshop.
type DispatchResult struct {
	ID         int64  `json:"id"`
	Status     string `json:"status"`
	HangarID   int64  `json:"hangar_id"`
	HangarName string `json:"hangar_name"`
	VehicleID  *int64 `json:"vehicle_id"`
	Plate      string `json:"license_plate,omitempty"`

	// AlreadyThere when the truck is inside the hangar's radius. The schedule
	// is still dispatched — the work is booked either way — but no route is
	// returned, because routing a vehicle to where it is standing draws a dot.
	AlreadyThere bool `json:"already_there"`

	DistanceKm *float64 `json:"distance_km,omitempty"`
	EtaMin     *float64 `json:"eta_min,omitempty"`
	// Geometry is [lon, lat], ready for a map as a GeoJSON LineString.
	Geometry [][2]float64 `json:"geometry,omitempty"`
	Provider string       `json:"provider,omitempty"`

	// TollRp is what the trip to the workshop costs in tolls, for THIS
	// vehicle's class. Nil when the class is unknown or no toll applies —
	// never zero, because zero asserts the roads are free and a missing axle
	// count is not that claim.
	TollRp *int64 `json:"toll_rp,omitempty"`
	// TollByGolongan is every class's figure, returned when this vehicle's own
	// class cannot be determined. 47% of the legacy fleet has no axle count,
	// and showing a range beats picking one silently.
	TollByGolongan map[int]int64 `json:"toll_by_golongan,omitempty"`
	// TollGolongan is the class TollRp was taken from, so a disputed figure
	// can be traced to an assumption rather than argued about blind.
	TollGolongan int `json:"toll_golongan,omitempty"`
	// TollNote explains an absent figure: no axles recorded, no toll on the
	// route, or pricing unavailable. An empty toll with no note would read as
	// "this trip is toll-free".
	TollNote string `json:"toll_note,omitempty"`
	// TollUnpricedGates names gates no tariff covered. When present, every
	// toll figure above is an UNDERSTATEMENT: those gates contributed nothing.
	// Shown so a cheap route cannot be confused with an unmeasured one — and
	// each name is a tariff someone can look up.
	TollUnpricedGates []string `json:"toll_unpriced_gates,omitempty"`

	// VehicleMarkedRepair is false when the vehicle was archived, since a
	// retired truck must not be quietly returned to the fleet's active list by
	// booking a service against it.
	VehicleMarkedRepair bool `json:"vehicle_marked_repair"`
}

// WithRouter attaches a routing provider, so a dispatch can answer "how far is
// the workshop from where the truck is now". Optional: without one the
// dispatch still happens and the route is simply absent.
func (h *Handler) WithRouter(r routing.Router) *Handler {
	h.router = r
	return h
}

// WithTollPricer attaches toll pricing from endpoints. Optional: without one a
// dispatch still happens and the route still comes back, with no toll figure.
//
// Kept for a provider that only takes endpoints. Prefer WithTollQuoter, which
// gets the route geometry and so can price from our own tables.
func (h *Handler) WithTollPricer(p routing.TollPricer) *Handler {
	h.toll = p
	return h
}

// WithTollQuoter attaches route-geometry toll pricing, which is what the
// in-house tables need: two endpoints cannot say which gates lie between them.
//
// Takes precedence over WithTollPricer where both are set.
func (h *Handler) WithTollQuoter(q routing.TollQuoter) *Handler {
	h.tollQuoter = q
	return h
}

// WithLive attaches the live-position service, which the route starts from.
func (h *Handler) WithLive(l *liveview.Service) *Handler {
	h.live = l
	return h
}

// dispatch godoc
//
//	@Summary	Send a vehicle to a hangar for a due service
//	@Description Marks the schedule in_service, the vehicle under repair, and returns the route from the vehicle's current position to the hangar, with its toll. `toll_rp` is for this vehicle's own class, derived from its axle count; when no axle count is recorded — 47% of the legacy fleet — `toll_by_golongan` lists every class instead and `toll_note` says why. An absent toll always carries a note, because a missing figure would otherwise read as a toll-free trip. Both routing and toll are computed in-house: Valhalla for the route, and FMS's own toll_gates/toll_tariffs for the price, by finding which gates the route passes and which charges they make up. A gate no tariff covers REFUSES the quote rather than returning a partial sum, because a plausible figure that is too low gets billed while a missing one gets noticed. An external provider remains wired as a fallback for routes the tables cannot yet price.
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int							true	"service schedule id"
//	@Param		body	body		maintenance.DispatchRequest	true	"hangar"
//	@Success	200		{object}	maintenance.DispatchResult
//	@Failure	409		{object}	map[string]string	"already in service"
//	@Router		/v1/service-schedules/{id}/dispatch [post]
func (h *Handler) dispatch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req DispatchRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.HangarID == 0 {
		httpx.Error(w, http.StatusBadRequest, "hangar_id is required: a dispatch needs a destination")
		return
	}

	tid := auth.TenantID(r.Context())

	var out DispatchResult
	var vehLat, vehLon float64
	var hangarLat, hangarLon float64
	var radiusM int32
	// The vehicle's toll class inputs, read in the same transaction as the
	// rest. Nil means unknown and must stay that way — see priceToll.
	var vehAxles, vehGolongan *int16

	// FIRST PASS: read what is needed and confirm the dispatch is legal, all
	// inside the tenant transaction so RLS (migration 0055) scopes it. The
	// route is computed AFTER this, because it is a network call and holding a
	// transaction open across one is how a slow provider becomes a lock.
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		s, err := st.GetServiceSchedule(r.Context(), id)
		if err != nil {
			return err
		}
		if s.Status == "in_service" {
			return errAlreadyDispatched
		}
		// Read straight from the table rather than adding a sqlc query for
		// one row: hangar.go does the same, and RLS on hangars scopes it.
		if err := tx.QueryRow(r.Context(),
			`SELECT name, lat, lon, radius_m FROM hangars WHERE id = $1 AND active`,
			req.HangarID,
		).Scan(&out.HangarName, &hangarLat, &hangarLon, &radiusM); err != nil {
			return err
		}
		out.VehicleID = s.VehicleID
		if s.LicensePlate != nil {
			out.Plate = *s.LicensePlate
		}
		return nil
	})
	switch {
	case errors.Is(err, errAlreadyDispatched):
		httpx.Error(w, http.StatusConflict,
			"this service is already in progress; complete it before dispatching again")
		return
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "no such service schedule or hangar")
		return
	case err != nil:
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	// The vehicle's axle count, for the toll class. A separate small query
	// rather than another join: it is needed only when a vehicle exists, and
	// folding it into the schedule read would complicate the common path for
	// one optional figure.
	if out.VehicleID != nil {
		if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
			return tx.QueryRow(r.Context(),
				`SELECT axles, golongan_override FROM vehicles WHERE id = $1`,
				*out.VehicleID).Scan(&vehAxles, &vehGolongan)
		}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			// Non-fatal: an unreadable axle count means the toll is reported
			// per class instead of for one, which priceToll already handles.
			vehAxles, vehGolongan = nil, nil
		}
	}

	// The route, if a vehicle and a provider are both available. A schedule
	// with no vehicle is legitimate (a component on order, say) and still
	// dispatchable; it simply has nothing to route.
	if out.VehicleID != nil && h.live != nil {
		vehicles, lerr := h.live.List(r.Context(), tid, nil)
		if lerr == nil {
			for i := range vehicles {
				v := &vehicles[i]
				if v.VehicleID != *out.VehicleID || v.Position == nil ||
					v.Position.Lat == nil || v.Position.Lon == nil {
					continue
				}
				vehLat, vehLon = *v.Position.Lat, *v.Position.Lon
				if metersBetween(vehLat, vehLon, hangarLat, hangarLon) <= float64(radiusM) {
					out.AlreadyThere = true
					break
				}
				if h.router == nil {
					break
				}
				route, rerr := h.router.Route(r.Context(),
					routing.Point{Lat: vehLat, Lon: vehLon},
					routing.Point{Lat: hangarLat, Lon: hangarLon})
				if rerr != nil {
					// A routing failure must NOT block the dispatch. Taking a
					// truck off the road is the decision being recorded; the
					// distance is useful context around it.
					break
				}
				km := float64(int(route.DistanceM/100+0.5)) / 10
				min := float64(int(route.DurationS/6+0.5)) / 10
				out.DistanceKm, out.EtaMin = &km, &min
				out.Geometry, out.Provider = route.Geometry, route.Provider

				// TOLL, priced on the route we just computed.
				//
				// Deliberately after the route and never fatal: the decision
				// being recorded is taking a truck off the road, and a toll
				// figure is context around it. A pricing outage must not stop
				// a dispatch, and an absent figure carries a note rather than
				// a zero — zero would assert the roads are free.
				h.priceToll(r.Context(), &out, vehAxles, vehGolongan,
					vehLat, vehLon, hangarLat, hangarLon)
				break
			}
		}
	}

	// SECOND PASS: record it. The UPDATE's own WHERE clause rejects a second
	// dispatch, so two operators clicking at once produce one dispatch and one
	// 409 rather than a silently overwritten record of when and where.
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)

		// dispatched_by is a foreign key into THIS database's users table, and
		// the signed-in operator may not be in it: an SSO session carries the
		// platform's user id from karlo_auth, which has no local row. Filling
		// it blindly fails the whole dispatch with
		//
		//   violates foreign key constraint "service_schedules_dispatched_by_fkey"
		//
		// so a truck could not be sent for its service at all, and the error
		// named a constraint rather than the problem. Same trap as
		// api_tokens_created_by_fkey, solved the same way: attribution is
		// worth having and not worth blocking on, so it is recorded when the
		// user is local and left null when they are not.
		var dispatchedBy *int64
		if uid := auth.UserID(r.Context()); uid > 0 {
			var exists bool
			if e := tx.QueryRow(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, uid).Scan(&exists); e == nil && exists {
				dispatchedBy = &uid
			}
		}

		row, err := st.DispatchServiceSchedule(r.Context(), store.DispatchServiceScheduleParams{
			ID: id, HangarID: &req.HangarID, DispatchedBy: dispatchedBy,
			DistanceKm: out.DistanceKm, EtaMin: out.EtaMin,
		})
		if err != nil {
			return err
		}
		out.ID, out.Status, out.HangarID = row.ID, row.Status, req.HangarID
		if row.VehicleID != nil {
			if err := st.SetVehicleStatus(r.Context(), store.SetVehicleStatusParams{
				ID: *row.VehicleID, Status: "repair",
			}); err != nil {
				return err
			}
			out.VehicleMarkedRepair = true
		}
		return nil
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusConflict,
			"this service is already in progress; complete it before dispatching again")
		return
	case err != nil:
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

var errAlreadyDispatched = errors.New("already in service")

// CompleteRequest closes a service out.
type CompleteRequest struct {
	// DoneAt defaults to today. Supplied when the paperwork lands later than
	// the work did, which is the normal case.
	DoneAt *string `json:"done_at"`
	// Odometer at completion. Rolls the next interval forward from the real
	// reading rather than from an estimate.
	Odometer *float64 `json:"odometer"`
	// NextDueAt and NextOdometer are computed by the caller from the
	// schedule's own interval. Left here rather than derived in SQL so the
	// arithmetic lives in one language, beside the rules that set it at
	// creation.
	NextDueAt    *string  `json:"next_due_at"`
	NextOdometer *float64 `json:"next_odometer"`
}

// complete godoc
//
//	@Summary	Finish a service and return the vehicle to the fleet
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path	int							true	"service schedule id"
//	@Param		body	body	maintenance.CompleteRequest	true	"completion"
//	@Success	200
//	@Failure	409	{object}	map[string]string	"not in service"
//	@Router		/v1/service-schedules/{id}/complete [post]
func (h *Handler) complete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req CompleteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	doneAt, err := conv.Date(req.DoneAt)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "done_at must be YYYY-MM-DD")
		return
	}
	nextDue, err := conv.Date(req.NextDueAt)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "next_due_at must be YYYY-MM-DD")
		return
	}

	tid := auth.TenantID(r.Context())
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		row, err := st.CompleteServiceSchedule(r.Context(), store.CompleteServiceScheduleParams{
			ID: id, DoneAt: doneAt, Odometer: conv.Num(req.Odometer),
			NextDueAt: nextDue, NextOdometer: conv.Num(req.NextOdometer),
		})
		if err != nil {
			return err
		}
		if row.VehicleID == nil {
			return nil
		}
		// Back on the road. SetVehicleStatus leaves 'archive' alone, so a
		// retired truck is not resurrected by closing an old schedule.
		return st.SetVehicleStatus(r.Context(), store.SetVehicleStatusParams{
			ID: *row.VehicleID, Status: "active",
		})
	})
	switch {
	case errors.Is(e, pgx.ErrNoRows):
		httpx.Error(w, http.StatusConflict, "this service is not in progress")
		return
	case e != nil:
		httpx.Error(w, http.StatusBadRequest, e.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// metersBetween is the great-circle distance, used only to answer "is the
// truck already at the hangar". A local copy rather than an import because
// hangar's is unexported, and a shared helper for eight lines of trigonometry
// would couple two packages for no gain.
func metersBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const earthM = 6371008.8
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dLat, dLon := p2-p1, (lon2-lon1)*math.Pi/180
	h := math.Pow(math.Sin(dLat/2), 2) + math.Cos(p1)*math.Cos(p2)*math.Pow(math.Sin(dLon/2), 2)
	return earthM * 2 * math.Asin(math.Sqrt(h))
}

// RouteToHangar is the journey from where a vehicle is NOW to its hangar.
type RouteToHangar struct {
	ScheduleID int64   `json:"schedule_id"`
	Status     string  `json:"status"`
	HangarID   int64   `json:"hangar_id"`
	HangarName string  `json:"hangar_name"`
	HangarLat  float64 `json:"hangar_lat"`
	HangarLon  float64 `json:"hangar_lon"`
	VehicleID  *int64  `json:"vehicle_id"`

	// VehicleLat/Lon are where the route starts: the vehicle's live position,
	// not where it stood when dispatched.
	VehicleLat *float64 `json:"vehicle_lat,omitempty"`
	VehicleLon *float64 `json:"vehicle_lon,omitempty"`

	// AlreadyThere when the vehicle is inside the hangar's radius. No route is
	// returned, because routing a vehicle to where it is standing draws a dot.
	AlreadyThere bool `json:"already_there"`

	DistanceKm *float64 `json:"distance_km,omitempty"`
	EtaMin     *float64 `json:"eta_min,omitempty"`
	// Geometry is [lon, lat], ready for a map as a GeoJSON LineString.
	Geometry [][2]float64 `json:"geometry,omitempty"`
	Provider string       `json:"provider,omitempty"`

	// Toll for the REMAINING journey, recomputed with the route. Same shape
	// and the same rules as the dispatch response: a single figure when the
	// vehicle's class is known, every class when it is not, and a note rather
	// than a zero whenever there is no figure.
	TollRp            *int64        `json:"toll_rp,omitempty"`
	TollByGolongan    map[int]int64 `json:"toll_by_golongan,omitempty"`
	TollGolongan      int           `json:"toll_golongan,omitempty"`
	TollNote          string        `json:"toll_note,omitempty"`
	TollUnpricedGates []string      `json:"toll_unpriced_gates,omitempty"`
}

// routeToHangar godoc
//
//	@Summary		The remaining journey from a vehicle to its hangar
//	@Description	Routes from the vehicle's CURRENT position to the hangar its service schedule names, with the toll for the remaining journey. Read-only and safe to poll: unlike the dispatch call it records nothing, which is what lets Live View draw the remaining path as the truck moves. The figures stored at dispatch are deliberately not reused — the vehicle has been driving since, so they describe a journey that no longer starts where it starts now. `already_there` means the vehicle is inside the hangar's radius and no line is returned.
//	@Tags			maintenance
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"service schedule id"
//	@Success		200	{object}	maintenance.RouteToHangar
//	@Failure		404	{object}	map[string]string	"no such schedule, or it has no hangar"
//	@Failure		409	{object}	map[string]string	"schedule has no vehicle to route"
//	@Failure		503	{object}	map[string]string	"routing not configured"
//	@Router			/v1/service-schedules/{id}/route [get]
func (h *Handler) routeToHangar(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid schedule id")
		return
	}
	tid := auth.EffectiveTenant(r)

	var out RouteToHangar
	out.ScheduleID = id
	var radiusM int32
	// RLS scopes this to the caller's tenant (0055), so an id from another
	// tenant returns no rows rather than another company's workshop.
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
			SELECT sc.status, sc.vehicle_id, sc.hangar_id, h.name, h.lat, h.lon, h.radius_m
			FROM service_schedules sc
			JOIN hangars h ON h.id = sc.hangar_id
			WHERE sc.id = $1`, id).Scan(&out.Status, &out.VehicleID, &out.HangarID,
			&out.HangarName, &out.HangarLat, &out.HangarLon, &radiusM)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		httpx.Error(w, http.StatusNotFound, "no such service schedule, or it has no hangar")
		return
	case err != nil:
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	if out.VehicleID == nil {
		httpx.Error(w, http.StatusConflict,
			"this schedule has no vehicle, so there is nothing to route")
		return
	}
	if h.live == nil || h.router == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"routing is not configured on this deployment")
		return
	}

	// One vehicle, not the fleet. The dispatch handler above lists every
	// vehicle and scans for one, which is wasteful there and would be worse
	// here because this endpoint is polled while the truck is en route.
	v, verr := h.live.One(r.Context(), tid, *out.VehicleID)
	if verr != nil {
		httpx.Error(w, http.StatusBadGateway, verr.Error())
		return
	}
	if v.Position == nil || v.Position.Lat == nil || v.Position.Lon == nil {
		// No fix: the hangar and the schedule are still worth returning, so
		// the map can show the destination without a line to it.
		httpx.JSON(w, http.StatusOK, out)
		return
	}
	out.VehicleLat, out.VehicleLon = v.Position.Lat, v.Position.Lon

	if metersBetween(*v.Position.Lat, *v.Position.Lon, out.HangarLat, out.HangarLon) <= float64(radiusM) {
		out.AlreadyThere = true
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	route, rerr := h.router.Route(r.Context(),
		routing.Point{Lat: *v.Position.Lat, Lon: *v.Position.Lon},
		routing.Point{Lat: out.HangarLat, Lon: out.HangarLon})
	if rerr != nil {
		httpx.Error(w, http.StatusBadGateway, rerr.Error())
		return
	}
	km := float64(int(route.DistanceM/100+0.5)) / 10
	min := float64(int(route.DurationS/6+0.5)) / 10
	out.DistanceKm, out.EtaMin = &km, &min
	out.Geometry, out.Provider = route.Geometry, route.Provider

	// Toll on the remaining journey. Read through a DispatchResult because
	// priceToll fills that shape; the two endpoints answer the same question
	// and must not drift into pricing it differently.
	var priced DispatchResult
	priced.Geometry = route.Geometry
	var axles, override *int16
	if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`SELECT axles, golongan_override FROM vehicles WHERE id = $1`,
			*out.VehicleID).Scan(&axles, &override)
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		axles, override = nil, nil
	}
	h.priceToll(r.Context(), &priced, axles, override,
		*v.Position.Lat, *v.Position.Lon, out.HangarLat, out.HangarLon)
	out.TollRp = priced.TollRp
	out.TollByGolongan = priced.TollByGolongan
	out.TollGolongan = priced.TollGolongan
	out.TollNote = priced.TollNote
	out.TollUnpricedGates = priced.TollUnpricedGates

	httpx.JSON(w, http.StatusOK, out)
}

// priceToll fills in the toll fields, or explains why it could not.
//
// WHY THE CLASS IS NOT GUESSED. Toll is keyed on golongan, which comes from the
// axle count — and 47% of the legacy fleet has none recorded, with some of the
// rest contradicting the vehicle (axle 3 on a pickup). Picking a class for
// those would misprice them invisibly, so a vehicle with no usable axle count
// gets every class's figure and a note, and the operator decides.
func (h *Handler) priceToll(ctx context.Context, out *DispatchResult,
	axles, override *int16, fromLat, fromLon, toLat, toLon float64) {
	var (
		quote *routing.TollQuote
		err   error
	)
	switch {
	case h.tollQuoter != nil && len(out.Geometry) >= 2:
		// PREFERRED. Pricing from our own tables needs to know which gates the
		// route passes, which only the geometry says.
		quote, err = h.tollQuoter.PriceRoute(ctx, out.Geometry)
	case h.toll != nil:
		quote, err = h.toll.PriceToll(ctx, [][2]float64{
			{fromLon, fromLat},
			{toLon, toLat},
		})
	default:
		return // not configured; silence is correct, there is nothing to say
	}
	if err != nil {
		out.TollNote = "toll could not be priced: " + err.Error()
		return
	}
	out.TollUnpricedGates = quote.UnpricedGates

	if len(quote.ByGolongan) == 0 {
		if len(quote.UnpricedGates) > 0 {
			// Gates were found and none could be priced. NOT reported as
			// toll-free: the route demonstrably uses toll roads, so the note
			// names what is missing instead.
			out.TollNote = fmt.Sprintf(
				"no tariff yet for %d gate(s) on this route: %s",
				len(quote.UnpricedGates), strings.Join(quote.UnpricedGates, ", "))
			return
		}
		// A real answer: the route stays off the network.
		out.TollNote = "no toll on this route"
		return
	}
	out.TollByGolongan = quote.ByGolongan

	gol := 0
	switch {
	case override != nil && *override >= 1 && *override <= 5:
		gol = int(*override)
	case axles != nil:
		gol = routing.Golongan(int(*axles))
	}
	if gol == 0 {
		out.TollNote = "no axle count recorded for this vehicle, so its toll " +
			"class is unknown — every class is listed"
		return
	}
	if amount, ok := quote.ByGolongan[gol]; ok {
		out.TollRp, out.TollGolongan = &amount, gol
		if len(quote.UnpricedGates) > 0 {
			// A PARTIAL TOTAL, SAID OUT LOUD. The figure is real as far as it
			// goes and useful for comparing two workshops, but it is short by
			// whatever the named gates charge — and a short total that looks
			// complete is the one way this feature could mislead.
			out.TollNote = fmt.Sprintf(
				"at least this much — no tariff yet for %s",
				strings.Join(quote.UnpricedGates, ", "))
		}
		return
	}
	// The class is known but the provider did not price it. Reported rather
	// than substituted from a neighbouring class, which would be a guess
	// wearing a precise number.
	out.TollNote = fmt.Sprintf("golongan %d was not priced for this route", gol)
}
