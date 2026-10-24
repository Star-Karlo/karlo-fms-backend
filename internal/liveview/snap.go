package liveview

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/routing"
)

// Snapping a travelled path to the road network.
//
// The history polyline joins raw GPS fixes with straight lines, which is
// honest but ugly: it cuts corners at junctions, wanders through buildings
// where the signal bounced, and scatters into a starburst wherever the vehicle
// sat still. Map matching replaces it with the roads the vehicle most probably
// used.
//
// WHY THIS IS A SEPARATE ENDPOINT rather than being folded into the history
// response. Matching costs a provider request and a second or two, while the
// raw history is free and instant. Most views of a trip never need it, so it is
// requested only when someone actually turns it on — and the raw trace stays
// available, because the matched line is an INTERPRETATION and there are times
// (an accident, a dispute, an unmapped yard) when the fixes themselves are what
// matters.

// WithMatcher attaches a map-matching provider. Optional: without one the snap
// endpoint answers 503 and history keeps working exactly as before.
func (h *Handler) WithMatcher(m routing.Matcher) *Handler {
	h.matcher = m
	return h
}

// SnappedHistory is a trip's path aligned to the road network.
type SnappedHistory struct {
	VehicleID int64        `json:"vehicle_id"`
	Geometry  [][2]float64 `json:"geometry"`
	// DistanceKm is measured along roads. Verified against a day whose distance
	// was known independently from the daily rollup: 489 km matched vs 461.5 km
	// recorded, i.e. ~6% over, which is what following real road geometry
	// instead of straight lines between fixes should produce.
	DistanceKm float64 `json:"distance_km"`
	PointsIn   int     `json:"points_in"`
	// ChunksMatched below ChunksTotal means part of the journey could not be
	// matched — typically an unmapped yard or plantation track. Surfaced so the
	// UI can say the line is incomplete rather than presenting a gap as fact.
	ChunksTotal   int    `json:"chunks_total"`
	ChunksMatched int    `json:"chunks_matched"`
	Provider      string `json:"provider"`

	// StraightLineKm is the great-circle distance through the raw fixes: the
	// shortest the journey could possibly have been.
	//
	// It exists because ChunksMatched == ChunksTotal does NOT mean the whole
	// path was found. A matcher can return a SHORT answer for every chunk and
	// report complete success — measured on Valhalla against a known route,
	// reconstruction falls to 83% once fixes are ~1 km apart and 28% at
	// ~1,5 km, with no error at any point. Our trackers report about every
	// 50 s, which is comfortable in town and marginal at motorway speed, so
	// this is the common case on long runs rather than an edge one.
	StraightLineKm float64 `json:"straight_line_km"`
	// Partial is set when the matched path is implausibly short against
	// StraightLineKm. Roads cannot be shorter than the straight lines between
	// their own points, so a matched distance well below it is arithmetically
	// impossible for a complete path — which makes this detectable rather than
	// something a reader has to notice.
	//
	// When true the line is a fragment: show it as incomplete, and do not use
	// DistanceKm as a distance.
	Partial bool `json:"partial"`
}

// snapHistory godoc
//
//	@Summary		A vehicle's travelled path, snapped to the road network
//	@Description	Map-matches the raw GPS trace so the line follows roads instead of cutting corners and wandering through buildings. Costs several provider requests (the trace is split into chunks of 200 fixes and matched in parallel), so it is requested explicitly rather than being folded into the history response — and the raw trace remains available, because the matched line is an interpretation and the fixes themselves are what matter in a dispute. `distance_km` is measured along roads. `chunks_matched` below `chunks_total` means part of the journey was unmatchable — typically an unmapped yard or plantation track.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			vehicle_id	path		int		true	"vehicle id"
//	@Param			from		query		string	true	"RFC3339 start"
//	@Param			to			query		string	true	"RFC3339 end"
//	@Success		200			{object}	liveview.SnappedHistory
//	@Failure		400			{object}	map[string]string
//	@Failure		503			{object}	map[string]string
//	@Router			/v1/vehicles/{vehicle_id}/history/snapped [get]
func (h *Handler) snapHistory(w http.ResponseWriter, r *http.Request) {
	if h.matcher == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"road snapping is not configured on this deployment")
		return
	}
	vehicleID, err := strconv.ParseInt(chi.URLParam(r, "vehicle_id"), 10, 64)
	if err != nil || vehicleID <= 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid vehicle id")
		return
	}
	from, err := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "from must be an RFC3339 timestamp")
		return
	}
	to, err := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "to must be an RFC3339 timestamp")
		return
	}
	if !to.After(from) {
		httpx.Error(w, http.StatusBadRequest, "to must be after from")
		return
	}

	// maxPoints 0: take EVERY fix, and do not thin them here or anywhere.
	// Matching quality depends on fixes staying at their recorded spacing —
	// thinning a long journey spreads them kilometres apart and the matcher
	// then joins them by the shortest path rather than the travelled one. The
	// matcher splits the trace into consecutive chunks instead.
	points, err := h.svc.History(r.Context(), auth.EffectiveTenant(r), vehicleID, from, to, 0)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	trace := make([]routing.TracePoint, 0, len(points))
	for _, p := range points {
		if p.Lat == nil || p.Lon == nil {
			continue
		}
		trace = append(trace, routing.TracePoint{Lat: *p.Lat, Lon: *p.Lon, At: p.Time})
	}
	if len(trace) < 2 {
		httpx.Error(w, http.StatusBadRequest,
			"this period has fewer than two positions, so there is no path to snap")
		return
	}

	matched, err := h.matcher.Match(r.Context(), trace)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, SnappedHistory{
		VehicleID:     vehicleID,
		Geometry:      matched.Geometry,
		DistanceKm:    float64(int(matched.DistanceM/100+0.5)) / 10,
		PointsIn:      matched.PointsIn,
		ChunksTotal:   matched.ChunksTotal,
		ChunksMatched: matched.ChunksMatched,
		Provider:      matched.Provider,

		// Both come from the matcher. Geoapify leaves RawDistanceM at zero, so
		// Partial stays false there — correct, since its failure mode is a
		// failed chunk rather than a short one, and that is already reported
		// through ChunksMatched.
		StraightLineKm: float64(int(matched.RawDistanceM/100+0.5)) / 10,
		Partial:        matched.Partial,
	})
}
