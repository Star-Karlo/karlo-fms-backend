package insight

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Peer-normalised utilisation: each vehicle against the median of its own group.
//
// Absolute distance says nothing on its own. A truck covering 90 km/day is
// either badly underused or working exactly as intended, and the only way to
// tell from telemetry alone is to compare it with vehicles doing the same job.
// Fleet group is that comparison set, falling back to vehicle type when a
// vehicle has no group.
//
// MEDIAN, NOT MEAN, and the reason is specific rather than stylistic: a fleet
// commonly contains a handful of vehicles that barely move — being repaired,
// awaiting sale, or fitted with a faulty tracker. Those zeros drag a mean down
// far enough that genuinely underused vehicles look average against it. The
// median ignores them.
//
// Groups smaller than minPeerGroup are not reported at all. "20% below the
// median of two vehicles" is a comparison with one other truck dressed up as
// a statistic.
const minPeerGroup = 4

// minWorkingKmPerDay is the bar for "this vehicle is actually in service".
//
// Ten kilometres a day averaged over the whole window is a very low bar — any
// working truck clears it easily — and it is deliberately low, because the
// point is only to separate vehicles that are working poorly (the finding) from
// vehicles that are not working at all (a different finding, in the unused
// vehicles report).
const minWorkingKmPerDay = 10.0

// PeerComparison is one vehicle measured against its group.
type PeerComparison struct {
	VehicleID    int64  `json:"vehicle_id"`
	LicensePlate string `json:"license_plate"`
	PeerGroup    string `json:"peer_group"`
	PeerCount    int    `json:"peer_count"`

	KmPerDay       float64 `json:"km_per_day"`
	PeerMedianKm   float64 `json:"peer_median_km_per_day"`
	DeviationPct   float64 `json:"deviation_pct"`
	IdlePct        float64 `json:"idle_pct"`
	PeerMedianIdle float64 `json:"peer_median_idle_pct"`

	Severity Severity `json:"severity"`
	Headline string   `json:"headline"`
}

// underThreshold is how far below the group median counts as a finding.
const underThreshold = -30.0

// utilisationPeers godoc
//
//	@Summary		Vehicles well below the median utilisation of their own group
//	@Description	Compares each vehicle's distance per active day against the median of its fleet group (or vehicle type when ungrouped). Groups of fewer than 4 are skipped — a "median" of two vehicles is not one. Median rather than mean, so parked or broken vehicles do not drag the comparison down and make genuinely underused ones look normal.
//	@Tags			insights
//	@Security		BearerAuth
//	@Produce		json
//	@Param			limit	query		int	false	"max findings (default 20)"
//	@Success		200		{array}		insight.PeerComparison
//	@Failure		503		{object}	map[string]string
//	@Router			/v1/insights/utilisation-peers [get]
func (h *Handler) utilisationPeers(w http.ResponseWriter, r *http.Request) {
	if !h.requireLive(w) {
		return
	}
	tenantID := auth.TenantID(r.Context())
	limit := intParam(r, "limit", 20, 1, 200)

	hist, err := h.fleetDaily(r.Context(), tenantID, totalWindow)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "fleet history unavailable: "+err.Error())
		return
	}

	type rated struct {
		v       liveview.VehicleTotals
		kmDay   float64
		idlePct float64
	}
	groups := map[string][]rated{}
	for _, v := range hist.Vehicles {
		// A vehicle that is not really working has no utilisation to compare.
		//
		// DaysUsed > 0 is not enough on its own: it counts any day the vehicle
		// moved at all, so a truck that rolled thirty metres in a yard qualifies
		// and then arrives here as "0 km/day, 100% below the median" — which is
		// true, useless, and drowns out the finding this endpoint exists for.
		// A vehicle that is parked, off-road or broken is the UNUSED VEHICLES
		// report; this one is for vehicles that are working, but less than their
		// peers. Excluding them also keeps them out of the median they would
		// otherwise drag down.
		if v.DaysUsed == 0 || v.DistanceKm/float64(v.DaysUsed) < minWorkingKmPerDay {
			continue
		}
		key := strOr(v.FleetGroup, "")
		if key == "" {
			key = strOr(v.VehicleType, "Ungrouped")
		}
		groups[key] = append(groups[key], rated{
			v:       v,
			kmDay:   v.DistanceKm / float64(v.DaysUsed),
			idlePct: idleShare(v.MovingH, v.IdleH),
		})
	}

	out := []PeerComparison{}
	for name, members := range groups {
		if len(members) < minPeerGroup {
			continue
		}
		kms := make([]float64, len(members))
		idles := make([]float64, len(members))
		for i, m := range members {
			kms[i] = m.kmDay
			idles[i] = m.idlePct
		}
		medKm := median(kms)
		medIdle := median(idles)
		if medKm <= 0.01 {
			continue // a whole group that barely moves: nothing to normalise against
		}

		for _, m := range members {
			dev, ok := pct(m.kmDay, medKm)
			if !ok || dev > underThreshold {
				continue
			}
			sev := SevInfo
			if dev <= underThreshold*2 {
				sev = SevWarning
			}
			out = append(out, PeerComparison{
				VehicleID: m.v.VehicleID, LicensePlate: m.v.LicensePlate,
				PeerGroup: name, PeerCount: len(members),
				KmPerDay: round1(m.kmDay), PeerMedianKm: round1(medKm),
				DeviationPct: round1(dev),
				IdlePct:      round1(m.idlePct), PeerMedianIdle: round1(medIdle),
				Severity: sev,
				Headline: fmt.Sprintf("%s covers %.0f km/day, %.0f%% below the %s median of %.0f",
					m.v.LicensePlate, m.kmDay, -dev, name, medKm),
			})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].DeviationPct < out[j].DeviationPct })
	if len(out) > limit {
		out = out[:limit]
	}
	httpx.JSON(w, http.StatusOK, out)
}

// median returns the middle value, averaging the two middles on an even count.
// Sorts a copy — the caller's slice order is meaningful elsewhere.
func median(in []float64) float64 {
	if len(in) == 0 {
		return 0
	}
	s := append([]float64(nil), in...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
