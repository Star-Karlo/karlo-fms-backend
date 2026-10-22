// Package site answers "which vehicles are at this place, since when, and for
// how long" for the two kinds of place a fleet draws on its map: geofences
// (watch a location) and hangars (send trucks to a location).
//
// WHY A VISIT AND NOT A FLAG. Presence used to be recomputed on every request
// by comparing the latest fix against a radius. That answers "is it there" and
// nothing else — not since when, not for how long, not what happened
// yesterday. The two screens that matter both ask the questions a boolean
// cannot answer, and the geofence report was reduced to parsing the TEXT of
// enter/exit alerts and pairing rows by the name it found in them, which comes
// apart the moment somebody renames a geofence.
//
// A visit is one row per vehicle per stay, opened on arrival and closed on
// departure. Presence is then "visits with no exit", duration is arithmetic on
// two timestamps, and the report is a SELECT over a window.
//
// THE DEBOUNCE. A circle drawn around a depot usually touches the road outside
// it, so trucks that never stop clip the fence and would otherwise produce an
// arrival and a departure a minute apart. min_dwell_min is how long a vehicle
// must stay before the visit counts: the row is opened at the first fix inside
// (the truck IS there, and the stay is measured from then) but stays
// provisional, and provisional visits are invisible to presence, to the report
// and to alerts. A pass-through opens and closes a row nobody ever sees.
package site

import (
	"context"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
)

// Kind distinguishes the two tables a visit can point at.
type Kind string

const (
	Geofence Kind = "geofence"
	Hangar   Kind = "hangar"
)

// Site is a named circle on the map, of either kind.
type Site struct {
	Kind    Kind    `json:"kind"`
	ID      int64   `json:"id"`
	Name    string  `json:"name"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m"`
	// MinDwellMin is the arrival debounce described in the package comment.
	// Hangars have none: a truck at the workshop gate is at the workshop.
	MinDwellMin int `json:"min_dwell_min"`
}

// Observation is one vehicle's position at one moment, as the collector saw it.
type Observation struct {
	VehicleID int64
	Lat, Lon  float64
	At        time.Time
}

// Transition is a confirmed arrival or departure, for the caller to turn into
// alerts. Provisional visits never produce one.
type Transition struct {
	Site      Site
	VehicleID int64
	// Exactly one of these is true.
	Entered bool
	Exited  bool
	At      time.Time
	// DwellMin is set on a departure: how long the stay lasted.
	DwellMin float64
}

// Visit is a stay, as the API reports it.
type Visit struct {
	ID           int64      `json:"id"`
	Kind         Kind       `json:"kind"`
	SiteID       int64      `json:"site_id"`
	SiteName     string     `json:"site_name"`
	VehicleID    int64      `json:"vehicle_id"`
	LicensePlate string     `json:"license_plate"`
	EnteredAt    time.Time  `json:"entered_at"`
	ExitedAt     *time.Time `json:"exited_at"`
	// DwellMin is the finished stay's length, or — while the vehicle is still
	// there — how long it has been there so far. The field a presence panel
	// shows, computed here so every caller agrees on it.
	DwellMin float64 `json:"dwell_min"`
	// Open says whether the vehicle is still inside.
	Open bool `json:"open"`
}

// LoadSites reads every active circle of both kinds for the current tenant.
// The caller supplies a transaction already scoped by RLS.
func LoadSites(ctx context.Context, tx pgx.Tx) ([]Site, error) {
	out := []Site{}
	rows, err := tx.Query(ctx, `
		SELECT 'geofence', id, name,
		       ST_Y(center::geometry)::float8, ST_X(center::geometry)::float8,
		       radius_m::float8, min_dwell_min
		  FROM geofences
		 WHERE center IS NOT NULL AND radius_m IS NOT NULL AND radius_m > 0 AND active
		UNION ALL
		SELECT 'hangar', id, name, lat, lon, radius_m::float8, 0
		  FROM hangars
		 WHERE active`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var s Site
		if err := rows.Scan(&s.Kind, &s.ID, &s.Name, &s.Lat, &s.Lon, &s.RadiusM, &s.MinDwellMin); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// openVisit is a row of site_visits with no exit.
type openVisit struct {
	id          int64
	kind        Kind
	siteID      int64
	vehicleID   int64
	enteredAt   time.Time
	confirmedAt *time.Time
	lastSeenAt  time.Time
}

func (v openVisit) key() visitKey { return visitKey{v.kind, v.siteID, v.vehicleID} }

type visitKey struct {
	kind      Kind
	siteID    int64
	vehicleID int64
}

// siteRef identifies a site without a vehicle attached to it.
type siteRef struct {
	kind Kind
	id   int64
}

// Apply reconciles the open visits with what the collector just observed, and
// returns the confirmed arrivals and departures.
//
// Only vehicles present in obs are considered for departure. A truck parked
// inside a geofence with its engine off stops reporting entirely, and treating
// silence as leaving would end every long stay — exactly the stays worth
// recording — a few minutes after it began.
func Apply(ctx context.Context, tx pgx.Tx, tenantID int64, sites []Site, obs []Observation) ([]Transition, error) {
	if len(sites) == 0 || len(obs) == 0 {
		return nil, nil
	}
	byKey := map[visitKey]bool{}
	seen := map[int64]bool{}
	at := map[int64]time.Time{}
	vehicleIDs := make([]int64, 0, len(obs))
	for _, o := range obs {
		seen[o.VehicleID] = true
		at[o.VehicleID] = o.At
		vehicleIDs = append(vehicleIDs, o.VehicleID)
		for _, s := range sites {
			if metersBetween(o.Lat, o.Lon, s.Lat, s.Lon) <= s.RadiusM {
				byKey[visitKey{s.Kind, s.ID, o.VehicleID}] = true
			}
		}
	}

	open := map[visitKey]openVisit{}
	rows, err := tx.Query(ctx, `
		SELECT id, geofence_id, hangar_id, vehicle_id, entered_at, confirmed_at, last_seen_at
		  FROM site_visits
		 WHERE exited_at IS NULL AND vehicle_id = ANY($1)`, vehicleIDs)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v openVisit
		var gid, hid *int64
		if err := rows.Scan(&v.id, &gid, &hid, &v.vehicleID, &v.enteredAt, &v.confirmedAt, &v.lastSeenAt); err != nil {
			rows.Close()
			return nil, err
		}
		switch {
		case gid != nil:
			v.kind, v.siteID = Geofence, *gid
		case hid != nil:
			v.kind, v.siteID = Hangar, *hid
		default:
			continue // the CHECK makes this unreachable
		}
		open[v.key()] = v
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Keyed by the site, not by (site, vehicle): the same twenty sites times
	// two thousand vehicles would be forty thousand map entries built and
	// thrown away on every pass.
	byRef := map[siteRef]Site{}
	for _, s := range sites {
		byRef[siteRef{s.Kind, s.ID}] = s
	}
	siteFor := func(k visitKey) (Site, bool) {
		s, ok := byRef[siteRef{k.kind, k.siteID}]
		return s, ok
	}

	var out []Transition

	// Arrivals and continuations.
	//
	// The continuations are batched into one statement. A depot with two
	// hundred trucks parked in it produces two hundred of them on EVERY pass,
	// for the rest of the time those trucks are there, and a round trip each
	// would make the quiet case — nothing happening — the expensive one.
	var touchIDs []int64
	var touchAt []time.Time
	var touchConfirm []bool
	for k := range byKey {
		s, ok := siteFor(k)
		if !ok {
			continue
		}
		now := at[k.vehicleID]
		v, isOpen := open[k]
		if !isOpen {
			// A new stay. It counts immediately when the site has no debounce.
			confirmed := s.MinDwellMin <= 0
			var confirmedAt *time.Time
			if confirmed {
				confirmedAt = &now
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO site_visits (tenant_id, geofence_id, hangar_id, vehicle_id,
				                         entered_at, confirmed_at, last_seen_at)
				VALUES ($1, $2, $3, $4, $5, $6, $5)
				ON CONFLICT DO NOTHING`,
				tenantID, nullIf(k.kind, Geofence, k.siteID), nullIf(k.kind, Hangar, k.siteID),
				k.vehicleID, now, confirmedAt); err != nil {
				return nil, err
			}
			if confirmed {
				out = append(out, Transition{Site: s, VehicleID: k.vehicleID, Entered: true, At: now})
			}
			continue
		}
		confirmNow := v.confirmedAt == nil && now.Sub(v.enteredAt) >= time.Duration(s.MinDwellMin)*time.Minute
		touchIDs = append(touchIDs, v.id)
		touchAt = append(touchAt, now)
		touchConfirm = append(touchConfirm, confirmNow)
		if confirmNow {
			out = append(out, Transition{Site: s, VehicleID: k.vehicleID, Entered: true, At: now})
		}
	}
	if len(touchIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE site_visits v
			   SET last_seen_at = u.at,
			       confirmed_at = COALESCE(v.confirmed_at, CASE WHEN u.confirm THEN u.at END),
			       updated_at   = now()
			  FROM unnest($1::bigint[], $2::timestamptz[], $3::boolean[]) AS u(id, at, confirm)
			 WHERE v.id = u.id`, touchIDs, touchAt, touchConfirm); err != nil {
			return nil, err
		}
	}

	// Departures: an open stay whose vehicle reported this pass, from outside.
	var closeIDs []int64
	for k, v := range open {
		if byKey[k] || !seen[k.vehicleID] {
			continue
		}
		closeIDs = append(closeIDs, v.id)
		if v.confirmedAt == nil {
			continue // provisional: it drove through, and nobody needs to know
		}
		// Closed at the LAST FIX INSIDE, not at the first fix outside. A
		// vehicle reporting every two minutes would otherwise be credited
		// with two minutes of standing on the far side of the fence.
		dwell := v.lastSeenAt.Sub(v.enteredAt).Minutes()
		if dwell < 0 {
			dwell = 0
		}
		s, ok := siteFor(k)
		if !ok {
			s = Site{Kind: k.kind, ID: k.siteID}
		}
		out = append(out, Transition{
			Site: s, VehicleID: k.vehicleID, Exited: true,
			At: v.lastSeenAt, DwellMin: round1(dwell),
		})
	}
	if len(closeIDs) > 0 {
		// dwell_min is computed in SQL from the row's own timestamps, so the
		// stored figure cannot disagree with the two times beside it.
		if _, err := tx.Exec(ctx, `
			UPDATE site_visits
			   SET exited_at  = last_seen_at,
			       dwell_min  = round((EXTRACT(EPOCH FROM (last_seen_at - entered_at)) / 60)::numeric, 1),
			       updated_at = now()
			 WHERE id = ANY($1) AND exited_at IS NULL`, closeIDs); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Overstay is an open, confirmed visit that has lasted past some threshold and
// has not been reported yet.
type Overstay struct {
	VisitID   int64
	Kind      Kind
	SiteID    int64
	SiteName  string
	VehicleID int64
	EnteredAt time.Time
	DwellMin  float64
}

// Overstays lists open confirmed visits longer than minMinutes that have not
// had an overstay alert yet. The threshold belongs to the notification rule,
// not the site, so it is passed in — one site can be watched by two rules with
// different patience.
func Overstays(ctx context.Context, tx pgx.Tx, minMinutes float64) ([]Overstay, error) {
	rows, err := tx.Query(ctx, `
		SELECT v.id, CASE WHEN v.geofence_id IS NOT NULL THEN 'geofence' ELSE 'hangar' END,
		       COALESCE(v.geofence_id, v.hangar_id),
		       COALESCE(g.name, h.name, ''), v.vehicle_id, v.entered_at,
		       EXTRACT(EPOCH FROM (now() - v.entered_at)) / 60
		  FROM site_visits v
		  LEFT JOIN geofences g ON g.id = v.geofence_id
		  LEFT JOIN hangars   h ON h.id = v.hangar_id
		 WHERE v.exited_at IS NULL
		   AND v.confirmed_at IS NOT NULL
		   AND v.dwell_alerted_at IS NULL
		   AND now() - v.entered_at >= make_interval(mins => $1::int)`, int(minMinutes))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Overstay{}
	for rows.Next() {
		var o Overstay
		if err := rows.Scan(&o.VisitID, &o.Kind, &o.SiteID, &o.SiteName, &o.VehicleID, &o.EnteredAt, &o.DwellMin); err != nil {
			return nil, err
		}
		o.DwellMin = round1(o.DwellMin)
		out = append(out, o)
	}
	return out, rows.Err()
}

// MarkOverstayAlerted stops a visit producing a second overstay alert.
func MarkOverstayAlerted(ctx context.Context, tx pgx.Tx, visitIDs []int64) error {
	if len(visitIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE site_visits SET dwell_alerted_at = now() WHERE id = ANY($1)`, visitIDs)
	return err
}

// Present lists the vehicles currently inside one site, longest stay first —
// the order that matters, since the truck that has been there all day is the
// one somebody needs to explain.
func Present(ctx context.Context, tx pgx.Tx, kind Kind, siteID int64) ([]Visit, error) {
	return query(ctx, tx, `
		  AND v.exited_at IS NULL AND v.confirmed_at IS NOT NULL
		  AND `+column(kind)+` = $1
		ORDER BY v.entered_at`, siteID)
}

// PresentCounts returns how many vehicles are inside each site of one kind.
func PresentCounts(ctx context.Context, tx pgx.Tx, kind Kind) (map[int64]int, error) {
	rows, err := tx.Query(ctx, `
		SELECT `+column(kind)+`, count(*)
		  FROM site_visits v
		 WHERE exited_at IS NULL AND confirmed_at IS NOT NULL AND `+column(kind)+` IS NOT NULL
		 GROUP BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// Window lists visits that overlap [from, to] for the given sites (all of that
// kind when ids is empty), oldest arrival first — the order a movement log is
// read in.
func Window(ctx context.Context, tx pgx.Tx, kind Kind, ids []int64, from, to time.Time) ([]Visit, error) {
	where := `
		  AND v.confirmed_at IS NOT NULL
		  AND ` + column(kind) + ` IS NOT NULL
		  AND v.entered_at <= $2
		  AND COALESCE(v.exited_at, now()) >= $1`
	args := []any{from, to}
	if len(ids) > 0 {
		where += ` AND ` + column(kind) + ` = ANY($3)`
		args = append(args, ids)
	}
	return query(ctx, tx, where+` ORDER BY v.entered_at`, args...)
}

// ForVehicle lists one vehicle's visits in a window, both kinds.
func ForVehicle(ctx context.Context, tx pgx.Tx, vehicleID int64, from, to time.Time) ([]Visit, error) {
	return query(ctx, tx, `
		  AND v.confirmed_at IS NOT NULL
		  AND v.vehicle_id = $1
		  AND v.entered_at <= $3
		  AND COALESCE(v.exited_at, now()) >= $2
		ORDER BY v.entered_at`, vehicleID, from, to)
}

func query(ctx context.Context, tx pgx.Tx, where string, args ...any) ([]Visit, error) {
	rows, err := tx.Query(ctx, `
		SELECT v.id,
		       CASE WHEN v.geofence_id IS NOT NULL THEN 'geofence' ELSE 'hangar' END,
		       COALESCE(v.geofence_id, v.hangar_id),
		       COALESCE(g.name, h.name, ''),
		       v.vehicle_id, COALESCE(veh.license_plate, ''),
		       v.entered_at, v.exited_at,
		       EXTRACT(EPOCH FROM (COALESCE(v.exited_at, now()) - v.entered_at)) / 60
		  FROM site_visits v
		  LEFT JOIN geofences g  ON g.id = v.geofence_id
		  LEFT JOIN hangars   h  ON h.id = v.hangar_id
		  LEFT JOIN vehicles  veh ON veh.id = v.vehicle_id
		 WHERE true`+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Visit{}
	for rows.Next() {
		var v Visit
		if err := rows.Scan(&v.ID, &v.Kind, &v.SiteID, &v.SiteName, &v.VehicleID, &v.LicensePlate,
			&v.EnteredAt, &v.ExitedAt, &v.DwellMin); err != nil {
			return nil, err
		}
		v.DwellMin = round1(v.DwellMin)
		v.Open = v.ExitedAt == nil
		out = append(out, v)
	}
	return out, rows.Err()
}

// CloseAllFor ends every open visit at a site, used when the site is edited in
// a way that changes who is inside it (moved, or its radius changed). Leaving
// them open would record a stay at a place that no longer exists where it did.
func CloseAllFor(ctx context.Context, tx pgx.Tx, kind Kind, siteID int64) error {
	_, err := tx.Exec(ctx, `
		UPDATE site_visits
		   SET exited_at = COALESCE(last_seen_at, now()),
		       dwell_min = round((EXTRACT(EPOCH FROM (COALESCE(last_seen_at, now()) - entered_at)) / 60)::numeric, 1),
		       updated_at = now()
		 WHERE exited_at IS NULL AND `+column(kind)+` = $1`, siteID)
	return err
}

func column(k Kind) string {
	if k == Hangar {
		return "v.hangar_id"
	}
	return "v.geofence_id"
}

func nullIf(k, want Kind, id int64) *int64 {
	if k != want {
		return nil
	}
	return &id
}

// metersBetween is the great-circle distance between two WGS84 points.
func metersBetween(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371000.0
	p := math.Pi / 180
	dLat, dLon := (lat2-lat1)*p, (lon2-lon1)*p
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*p)*math.Cos(lat2*p)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

func round1(f float64) float64 { return math.Round(f*10) / 10 }
