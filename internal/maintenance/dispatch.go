package maintenance

import (
	"errors"
	"math"
	"net/http"
	"strconv"

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

// WithLive attaches the live-position service, which the route starts from.
func (h *Handler) WithLive(l *liveview.Service) *Handler {
	h.live = l
	return h
}

// dispatch godoc
//
//	@Summary	Send a vehicle to a hangar for a due service
//	@Description Marks the schedule in_service, the vehicle under repair, and returns the route from the vehicle's current position to the hangar.
//	@Tags		maintenance
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int							true	"service schedule id"
//	@Param		body	body		maintenance.DispatchRequest	true	"hangar"
//	@Success	200		{object}	maintenance.DispatchResult
//	@Failure	409		{object}	httpx.ErrorBody	"already in service"
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
//	@Failure	409	{object}	httpx.ErrorBody	"not in service"
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
