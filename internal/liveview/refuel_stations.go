package liveview

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// promoteAtStations lets a known filling station vouch for a small fill.
//
// WHY THIS EXISTS. The sensor-only rule needs a climb of refuelMinPct — about
// 41 litres on a 340-litre tank — before it believes fuel arrived. That floor
// is there to keep a noisy sender out of the figures, and it costs every
// partial fill below it: a driver puts in 20 litres, the gauge moves six
// points, and the balance quietly loses both the fill and the litres.
//
// Where the truck was parked at an SPBU while that climb happened, the floor
// is not needed. The station is evidence that does not come from the sensor,
// so a climb of riseMinPct is enough.
//
// WHAT IT DELIBERATELY DOES NOT DO. It never creates a refuel. A stop at a
// filling station is also lunch, a toilet and a mandatory rest — Rest Area Km
// 754 is all four — and a truck that stopped without fuelling must not be
// charged for a tank it never bought. There must be a climb; the station only
// lowers what size of climb is believed.
//
// It is also not required: the fill that prompted all of this happened at a
// LOADING POINT from a company bowser, hundreds of kilometres from the nearest
// SPBU. A station nearby raises confidence; its absence proves nothing.
//
// Best effort. A failed lookup leaves the sensor-only answer, which is what
// this returned before stations were consulted at all — a fuel figure must not
// depend on a reference table being reachable.
func (s *Service) promoteAtStations(ctx context.Context, tenantID int64, cands []riseCandidate) []RefuelEvent {
	var pending []int
	for i, c := range cands {
		if !c.accepted && c.dwellMin >= stationDwellMin && c.event.Lat != nil && c.event.Lon != nil {
			pending = append(pending, i)
		}
	}

	if len(pending) > 0 {
		if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
			for _, i := range pending {
				var name, brand string
				// fuel_station is shared reference data with no tenant_id, so
				// there is nothing for RLS to filter; the transaction exists
				// so the tenant GUC is set exactly as it is everywhere else.
				err := tx.QueryRow(ctx, `
					SELECT coalesce(name,''), brand
					FROM fuel_station
					WHERE ST_DWithin(geog, ST_SetSRID(ST_MakePoint($1,$2),4326)::geography, $3)
					ORDER BY geog <-> ST_SetSRID(ST_MakePoint($1,$2),4326)::geography
					LIMIT 1`,
					*cands[i].event.Lon, *cands[i].event.Lat, stationRadiusM).Scan(&name, &brand)
				if err == pgx.ErrNoRows {
					continue
				}
				if err != nil {
					return err
				}
				cands[i].accepted = true
				// Name the pump rather than the road it is off. "SPBU
				// Pertamina 54.612.03" is something a fleet manager can
				// check against a receipt; "Jalan Tol Surabaya-Gempol" is not.
				if name != "" {
					cands[i].event.Address = name
				} else if brand != "" {
					cands[i].event.Address = brand
				}
			}
			return nil
		}); err != nil {
			slog.WarnContext(ctx, "fuel-station lookup failed; small fills stay unconfirmed",
				"error", err, "candidates", len(pending))
		}
	}

	var out []RefuelEvent
	for _, c := range cands {
		if c.accepted {
			out = append(out, c.event)
		}
	}
	return out
}
