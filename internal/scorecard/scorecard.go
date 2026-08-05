// Package scorecard computes monthly driver safety scores from behavior events.
package scorecard

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
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
	// Five accelerometer pillars, 20 points each (sum 100). The score is now a
	// driving-behaviour model derived from the tracker/camera G-sensor events
	// (internal/driverscore): X = Braking/Stopping, Y = Turning/Steering, plus a
	// Smoothness index. Steering is approximate in v1 (a low-speed cornering
	// proxy) until road/bearing context separates it from Turning cleanly.
	Weights: []Weight{
		{"BRAKING", "Braking", "accel", 20},
		{"STOPPING", "Stopping", "accel", 20},
		{"TURNING", "Turning", "accel", 20},
		{"STEERING", "Steering (approx.)", "accel", 20},
		{"SMOOTHNESS", "Smoothness", "accel", 20},
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
	// Meta carries the numbers behind the penalty — the specific dates an issue
	// occurred, the idle percentage — so the detail view can show WHEN and WHY,
	// not just how many. Shape varies by code; the client reads what it knows.
	Meta map[string]any `json:"meta,omitempty"`
}

// Detail is a single driver's monthly scorecard.
type Detail struct {
	DriverID   int64            `json:"driver_id"`
	TotalScore float64          `json:"total_score"`
	Index      string           `json:"index"`
	TripKm     float64          `json:"trip_km"`
	Behaviors  []DetailBehavior `json:"behaviors"`
}

// Recomputer recomputes a tenant's current-month scores on demand. The
// driverscore collector satisfies it; kept as an interface so this package does
// not import that one (which imports liveview, which would be a heavier graph).
type Recomputer interface {
	RunTenantNow(ctx context.Context, tenantID int64) error
}

// Handler exposes scorecard endpoints.
type Handler struct {
	pool  *db.Pool
	recmp Recomputer
}

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// WithRecomputer wires the "Recalculate scores" button. Optional: without it
// that endpoint answers 503 and reading scores is unaffected.
func (h *Handler) WithRecomputer(r Recomputer) *Handler { h.recmp = r; return h }

// Routes mounts scorecard endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/driver-scorecard/config", h.cfg)
	r.Get("/v1/driver-scorecard/cohorts", h.cohorts)
	r.Get("/v1/driver-scorecard", h.summary)
	r.Get("/v1/driver-scorecard/{id}", h.detail)
	r.Post("/v1/driver-scorecard/recalculate", h.recalculate)
}

// CohortFactor is one behaviour's average drag on one grade cohort.
type CohortFactor struct {
	Code    string  `json:"code"`
	Name    string  `json:"name"`
	AvgLost float64 `json:"avg_lost"`
}

// Cohort is one grade band and why its drivers landed there.
type Cohort struct {
	Grade   string         `json:"grade"`
	Drivers int            `json:"drivers"`
	AvgLost float64        `json:"avg_lost"`
	Factors []CohortFactor `json:"factors"`
}

// cohorts godoc
//
//	@Summary		Why each grade cohort landed where it did
//	@Description	Groups drivers by grade and reports, per grade, the average points each behaviour cost — the answer to "why are there so many B drivers". A cohort dragged down mostly by idling reads very differently from one dragged down by speeding, and only this shows which.
//	@Tags			scorecard
//	@Security		BearerAuth
//	@Produce		json
//	@Param			period	query	string	false	"YYYY-MM"
//	@Success		200	{array}	scorecard.Cohort
//	@Router			/v1/driver-scorecard/cohorts [get]
func (h *Handler) cohorts(w http.ResponseWriter, r *http.Request) {
	pd := period(r)
	tid := auth.TenantID(r.Context())
	nameByCode := map[string]string{}
	for _, wt := range config.Weights {
		nameByCode[wt.Code] = wt.Name
	}

	// grade -> code -> summed penalty, plus a count of drivers per grade.
	type acc struct {
		drivers   int
		totalLost float64
		byCode    map[string]float64
	}
	cohorts := map[string]*acc{}

	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		// One pass: each driver's penalty per behaviour, and their total (which
		// fixes their grade). Grading in Go keeps the band thresholds in one
		// place — config.IndexBands — rather than duplicated in SQL.
		rows, err := tx.Query(r.Context(), `
SELECT driver_id, event_type, coalesce(sum(score),0)::float8
FROM driver_score_events
WHERE tenant_id = $1 AND period = $2 AND event_type <> 'trip_km'
GROUP BY driver_id, event_type`, tid, pd.Time)
		if err != nil {
			return err
		}
		defer rows.Close()

		perDriver := map[int64]map[string]float64{}
		for rows.Next() {
			var did int64
			var code string
			var lost float64
			if err := rows.Scan(&did, &code, &lost); err != nil {
				return err
			}
			if perDriver[did] == nil {
				perDriver[did] = map[string]float64{}
			}
			perDriver[did][code] = lost
		}
		if err := rows.Err(); err != nil {
			return err
		}

		for _, codes := range perDriver {
			var total float64
			for _, v := range codes {
				total += v
			}
			grade := indexFor(clampScore(100 - total))
			a := cohorts[grade]
			if a == nil {
				a = &acc{byCode: map[string]float64{}}
				cohorts[grade] = a
			}
			a.drivers++
			a.totalLost += total
			for code, v := range codes {
				a.byCode[code] += v
			}
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Emit A..E in order, only bands that have drivers, factors biggest-first.
	out := []Cohort{}
	for _, band := range config.IndexBands {
		a := cohorts[band.Index]
		if a == nil || a.drivers == 0 {
			continue
		}
		c := Cohort{Grade: band.Index, Drivers: a.drivers,
			AvgLost: round1(a.totalLost / float64(a.drivers))}
		for code, sum := range a.byCode {
			avg := sum / float64(a.drivers)
			if avg < 0.05 {
				continue
			}
			c.Factors = append(c.Factors, CohortFactor{Code: code, Name: nameByCode[code], AvgLost: round1(avg)})
		}
		sort.Slice(c.Factors, func(i, j int) bool { return c.Factors[i].AvgLost > c.Factors[j].AvgLost })
		out = append(out, c)
	}
	httpx.JSON(w, http.StatusOK, out)
}

func round1(f float64) float64 { return float64(int(f*10+0.5)) / 10 }

// recalculate godoc
//
//	@Summary		Recompute this month's scores now
//	@Description	Runs the scoring collector for the current month immediately instead of waiting for the nightly pass — used after assigning drivers or fixing the registry. Idempotent: it recomputes the whole month, so pressing it twice changes nothing the second time.
//	@Tags			scorecard
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	map[string]string
//	@Failure		503	{object}	map[string]string
//	@Router			/v1/driver-scorecard/recalculate [post]
func (h *Handler) recalculate(w http.ResponseWriter, r *http.Request) {
	if h.recmp == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "scoring is not configured on this deployment")
		return
	}
	if err := h.recmp.RunTenantNow(r.Context(), auth.TenantID(r.Context())); err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"status": "recalculated"})
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
			// pgx decodes jsonb as []byte here; unmarshal to the map the client
			// reads. A map value (some drivers) is passed through as-is.
			var meta map[string]any
			switch mv := row.Metadata.(type) {
			case []byte:
				if len(mv) > 0 {
					_ = json.Unmarshal(mv, &meta)
				}
			case map[string]any:
				meta = mv
			}
			byCode[code] = DetailBehavior{Code: code, Name: nameByCode[code],
				Occurrences: row.Occurrences, Score: numF(row.Score), Meta: meta}
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
