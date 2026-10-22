package telemetrysync

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/site"
)

// Who gets told when a truck arrives somewhere.
//
// Two answers coexist here on purpose. Attaching a vehicle to a geofence has
// always meant "alert me about this pair", and accounts have set that up; a
// notification rule is the general way to say the same thing — all geofences
// or a chosen few, all vehicles or a chosen few, and with an overstay
// threshold that attachment cannot express. Either is enough to produce an
// alert, so adding rules took nothing away from accounts that never make one.
//
// The visits themselves are recorded regardless (see internal/site): presence
// and the movement report must work for a geofence nobody has subscribed to.

// pair is a (vehicle, geofence) attachment.
type pair struct{ vehicleID, geofenceID int64 }

// gfRule is one active geofence notification rule.
type gfRule struct {
	code      string
	urgency   string
	threshold *float64
	// Empty means "every one of them" in both cases — a rule created with no
	// geofence chosen watches the lot, which is what "Geofence IN" with
	// nothing else filled in plainly means.
	geofences map[int64]bool
	vehicles  map[int64]bool
}

// covers reports whether this rule speaks for a (geofence, vehicle) pair.
func (r gfRule) covers(geofenceID, vehicleID int64) bool {
	if len(r.geofences) > 0 && !r.geofences[geofenceID] {
		return false
	}
	if len(r.vehicles) > 0 && !r.vehicles[vehicleID] {
		return false
	}
	return true
}

type gfRules []gfRule

func (rs gfRules) byCode(code string) []gfRule {
	var out []gfRule
	for _, r := range rs {
		if r.code == code {
			out = append(out, r)
		}
	}
	return out
}

// transitionAlerts turns confirmed arrivals and departures into alert writes.
func (rs gfRules) transitionAlerts(trans []site.Transition, assigned map[pair]bool) []alertWrite {
	var out []alertWrite
	for _, t := range trans {
		if t.Site.Kind != site.Geofence {
			continue // hangar arrivals are shown, not announced
		}
		code := "GEOFENCE_ENTER"
		if t.Exited {
			code = "GEOFENCE_EXIT"
		}
		urgency, ok := "important", assigned[pair{t.VehicleID, t.Site.ID}]
		for _, r := range rs.byCode(code) {
			if r.covers(t.Site.ID, t.VehicleID) {
				ok = true
				if r.urgency != "" {
					urgency = r.urgency
				}
				break
			}
		}
		if !ok {
			continue
		}
		name := "Entered " + t.Site.Name
		if t.Exited {
			name = fmt.Sprintf("Left %s after %s", t.Site.Name, humanMinutes(t.DwellMin))
		}
		vid := t.VehicleID
		out = append(out, alertWrite{
			vid: vid, code: code, name: name, severity: urgency,
			cooldown: geofenceCooldownMin,
		})
	}
	return out
}

// loadGeofenceRules reads the active geofence rules for the current tenant.
func loadGeofenceRules(ctx context.Context, tx pgx.Tx) (gfRules, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.id, r.alert_code, COALESCE(r.urgency,''), r.threshold::float8, r.target
		  FROM notification_rules r
		 WHERE r.status = 'active'
		   AND r.alert_code IN ('GEOFENCE_ENTER','GEOFENCE_EXIT','GEOFENCE_DWELL')`)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*gfRule{}
	order := []int64{}
	for rows.Next() {
		var id int64
		var r gfRule
		var target []byte
		if err := rows.Scan(&id, &r.code, &r.urgency, &r.threshold, &target); err != nil {
			rows.Close()
			return nil, err
		}
		r.geofences = map[int64]bool{}
		r.vehicles = map[int64]bool{}
		// target is the same jsonb the other rules use; geofence_ids is this
		// rule type's own key and is simply absent on every other kind.
		var t struct {
			GeofenceIDs []int64 `json:"geofence_ids"`
		}
		if len(target) > 0 {
			_ = json.Unmarshal(target, &t)
		}
		for _, g := range t.GeofenceIDs {
			r.geofences[g] = true
		}
		byID[id] = &r
		order = append(order, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(byID) == 0 {
		return nil, nil
	}

	vrows, err := tx.Query(ctx, `SELECT rule_id, vehicle_id FROM rule_vehicles WHERE rule_id = ANY($1)`, order)
	if err != nil {
		return nil, err
	}
	defer vrows.Close()
	for vrows.Next() {
		var rid, vid int64
		if err := vrows.Scan(&rid, &vid); err != nil {
			return nil, err
		}
		if r, ok := byID[rid]; ok {
			r.vehicles[vid] = true
		}
	}
	if err := vrows.Err(); err != nil {
		return nil, err
	}

	out := make(gfRules, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

// humanMinutes writes a duration the way the alert will be read, not as a
// decimal: "3 h 20 min" rather than "200.4".
func humanMinutes(min float64) string {
	m := int(math.Round(min))
	switch {
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m%60 == 0:
		return fmt.Sprintf("%d h", m/60)
	default:
		return fmt.Sprintf("%d h %d min", m/60, m%60)
	}
}
