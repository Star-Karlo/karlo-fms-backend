// Package scorecard computes monthly driver safety scores from behavior events.
package scorecard

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Weight is one behavior's contribution to the score (percent).
type Weight struct {
	Code   string `json:"code"`
	Name   string `json:"name"`
	Group  string `json:"group"` // gps | dashcam
	Weight int    `json:"weight"`
}

// Config is the scoring-engine configuration (weights sum to 100).
type Config struct {
	MinDistanceKm int      `json:"min_distance_km"`
	AlertLimitPct int      `json:"alert_limit_pct"`
	Weights       []Weight `json:"weights"`
	IndexBands    []Band   `json:"index_bands"`
}

// Band is an index letter grade over a score range.
type Band struct {
	Index string `json:"index"`
	Min   int    `json:"min"`
	Max   int    `json:"max"`
}

var config = Config{
	MinDistanceKm: 300,
	AlertLimitPct: 5,
	// Weights sum to 100 across the THREE behaviours the 90-second GPS feed can
	// actually measure. The original set spread 100 points across harsh events
	// and dashcam signals — 75 of the 100 — none of which the devices report
	// today. With those unmeasured every driver scored 75-100 and the board
	// could not tell anyone apart, which is a scorecard that grades nothing.
	//
	// The unmeasured codes are kept at weight 0 rather than deleted: they are
	// what a full score WOULD include once the trackers are configured to emit
	// harsh-event and DMS data, and re-enabling them is then a weight change
	// rather than a rebuild. See internal/driverscore for how the three live
	// ones are derived, and the package doc there for why the rest cannot be.
	Weights: []Weight{
		{"OVERSPEED", "Speeding", "gps", 45},
		{"EXCEEDED_IDLE", "Excessive Idling", "gps", 30},
		{"LONG_SHIFT", "Long Shifts (fatigue)", "gps", 25},

		// Unmeasured at 90s sampling / without DMS. Present so the UI shows what
		// is not yet covered, and so enabling device reporting is a weight edit.
		{"HARSH_ACCEL", "Sudden Acceleration", "gps", 0},
		{"HARSH_BRAKING", "Sudden Deceleration", "gps", 0},
		{"SHARP_TURN", "Sharp Turns", "gps", 0},
		{"DMS_YAWN", "Yawning", "dashcam", 0},
		{"DMS_EYE_CLOSED", "Eye Closed", "dashcam", 0},
		{"DMS_PHONE", "Calling", "dashcam", 0},
		{"DMS_DISTRACTION", "Distraction", "dashcam", 0},
		{"DMS_OUT_OF_FRAME", "Out Of Frame", "dashcam", 0},
	},
	IndexBands: []Band{
		{"A", 91, 100}, {"B", 71, 90}, {"C", 51, 70}, {"D", 31, 50}, {"E", 0, 30},
	},
}

func indexFor(score float64) string {
	for _, b := range config.IndexBands {
		if int(score) >= b.Min && int(score) <= b.Max {
			return b.Index
		}
	}
	return "E"
}

func clampScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// SummaryRow is one driver's monthly score.
type SummaryRow struct {
	DriverID    int64   `json:"driver_id"`
	DriverName  string  `json:"driver_name"`
	Status      string  `json:"driver_status"`
	TotalScore  float64 `json:"total_score"`
	Index       string  `json:"index"`
	TripKm      float64 `json:"trip_km"`
	Occurrences int64   `json:"total_occurrences"`
}

// DetailBehavior is a per-behavior breakdown entry.
type DetailBehavior struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	Occurrences int64   `json:"occurrences"`
	Score       float64 `json:"score"`
}

// Detail is a single driver's monthly scorecard.
type Detail struct {
	DriverID   int64            `json:"driver_id"`
	TotalScore float64          `json:"total_score"`
	Index      string           `json:"index"`
	TripKm     float64          `json:"trip_km"`
	Behaviors  []DetailBehavior `json:"behaviors"`
}

// Handler exposes scorecard endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts scorecard endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/driver-scorecard/config", h.cfg)
	r.Get("/v1/driver-scorecard", h.summary)
	r.Get("/v1/driver-scorecard/{id}", h.detail)
}

// period parses ?period=YYYY-MM into the first-of-month date (default: current).
func period(r *http.Request) pgtype.Date {
	v := r.URL.Query().Get("period")
	t := time.Now()
	if v != "" {
		if p, err := time.Parse("2006-01", v); err == nil {
			t = p
		}
	}
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC), Valid: true}
}

// cfg godoc
//
//	@Summary	Driver-score engine configuration (weights + index bands)
//	@Tags		scorecard
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	scorecard.Config
//	@Router		/v1/driver-scorecard/config [get]
func (h *Handler) cfg(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, config)
}

// summary godoc
//
//	@Summary	Monthly driver scorecard (all drivers)
//	@Tags		scorecard
//	@Security	BearerAuth
//	@Produce	json
//	@Param		period	query		string	false	"YYYY-MM"
//	@Success	200	{object}	httpx.List[scorecard.SummaryRow]
//	@Router		/v1/driver-scorecard [get]
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	pd := period(r)
	tid := auth.TenantID(r.Context())
	var items []SummaryRow
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		rows, err := store.New(tx).DriverScoreSummary(r.Context(), pd)
		if err != nil {
			return err
		}
		items = make([]SummaryRow, 0, len(rows))
		for _, d := range rows {
			penalty := numF(d.TotalScore)
			final := clampScore(100 - penalty)
			items = append(items, SummaryRow{
				DriverID: d.DriverID, DriverName: d.DriverName, Status: d.DriverStatus,
				TotalScore: final, Index: indexFor(final), TripKm: numF(d.TripKm), Occurrences: d.TotalOccurrences,
			})
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[SummaryRow]{Items: items, Total: int64(len(items))})
}

// detail godoc
//
//	@Summary	One driver's monthly scorecard breakdown
//	@Tags		scorecard
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id		path		int		true	"driver id"
//	@Param		period	query		string	false	"YYYY-MM"
//	@Success	200	{object}	scorecard.Detail
//	@Router		/v1/driver-scorecard/{id} [get]
func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	pd := period(r)
	tid := auth.TenantID(r.Context())
	nameByCode := map[string]string{}
	for _, wt := range config.Weights {
		nameByCode[wt.Code] = wt.Name
	}
	var det Detail
	det.DriverID = id
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		rows, err := store.New(tx).DriverScoreDetail(r.Context(), store.DriverScoreDetailParams{DriverID: &id, Period: pd})
		if err != nil {
			return err
		}
		byCode := map[string]DetailBehavior{}
		var penalty, trip float64
		for _, row := range rows {
			code := ""
			if row.EventType != nil {
				code = *row.EventType
			}
			if code == "trip_km" {
				trip = numF(row.Score)
				continue
			}
			penalty += numF(row.Score)
			byCode[code] = DetailBehavior{Code: code, Name: nameByCode[code], Occurrences: row.Occurrences, Score: numF(row.Score)}
		}
		// Emit all 12 behaviors in canonical order (zero-filled).
		for _, wt := range config.Weights {
			if b, ok := byCode[wt.Code]; ok {
				det.Behaviors = append(det.Behaviors, b)
			} else {
				det.Behaviors = append(det.Behaviors, DetailBehavior{Code: wt.Code, Name: wt.Name})
			}
		}
		det.TripKm = trip
		det.TotalScore = clampScore(100 - penalty)
		det.Index = indexFor(det.TotalScore)
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusInternalServerError, e.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, det)
}

func numF(n pgtype.Numeric) float64 {
	if v := conv.NumF(n); v != nil {
		return *v
	}
	return 0
}
