package alert

import (
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// How many incidents did each truck have, and of what kind?
//
// Answerable from /v1/alerts only by reading every row, and that list caps
// at 500 — so a consumer counting client-side is right on a quiet fleet and
// quietly short on a busy one, which is the worst way to be wrong: it
// degrades with load, so the number is least trustworthy exactly when
// somebody is looking at it because something went wrong.
//
// Counted in Postgres instead. No row limit, because the answer is the same
// size whether the day held ten incidents or ten thousand.

// VehicleAlertCounts is one vehicle's tally for the window.
type VehicleAlertCounts struct {
	VehicleID    int64  `json:"vehicle_id"`
	LicensePlate string `json:"license_plate"`
	// Counts is keyed by ALERT CODE (OVERSPEED, HARSH_BRAKING, …), not by any
	// consumer's categories: a caller groups codes into its own kinds, and
	// this service does not inherit a vocabulary it cannot test.
	Counts map[string]int64 `json:"counts"`
	Total  int64            `json:"total"`
}

// AlertSummary is the whole fleet for one window.
type AlertSummary struct {
	From string `json:"from"`
	To   string `json:"to"`
	// ByCode is the fleet-wide tally, for a headline that does not need the
	// per-vehicle breakdown summed client-side.
	ByCode map[string]int64     `json:"by_code"`
	Items  []VehicleAlertCounts `json:"items"`
	Total  int64                `json:"total"`
}

// summary godoc
//
//	@Summary		Alert counts per vehicle per code over a window
//	@Description	The fleet's incidents for a window, counted in the database: one entry per vehicle with a map of alert code to count, plus the fleet-wide tally. Checked and unchecked alike — the question is what happened, not what is outstanding. There is NO ROW LIMIT here, which is the point: /v1/alerts caps at 500 rows, so counting that list is accurate only until a fleet has a busy day. Scoped to the caller's account (?account_id for platform staff) exactly as /v1/alerts is. Covers alerts still in the database; anything aged out to cold storage must be restored first.
//	@Tags			alerts
//	@Security		BearerAuth
//	@Produce		json
//	@Param			from	query		string	true	"RFC3339 window start"
//	@Param			to		query		string	false	"RFC3339 window end (default now)"
//	@Success		200		{object}	alert.AlertSummary
//	@Failure		400		{object}	map[string]string
//	@Router			/v1/alerts/summary [get]
func (h *Handler) summary(w http.ResponseWriter, r *http.Request) {
	to := time.Now()
	if v := r.URL.Query().Get("to"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "to must be RFC3339")
			return
		}
		to = t
	}
	v := r.URL.Query().Get("from")
	if v == "" {
		httpx.Error(w, http.StatusBadRequest, "from is required (RFC3339)")
		return
	}
	from, err := time.Parse(time.RFC3339, v)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "from must be RFC3339")
		return
	}
	if !from.Before(to) {
		httpx.Error(w, http.StatusBadRequest, "from must be before to")
		return
	}

	out := AlertSummary{
		From:   from.Format(time.RFC3339),
		To:     to.Format(time.RFC3339),
		ByCode: map[string]int64{},
		Items:  []VehicleAlertCounts{},
	}
	byVehicle := map[int64]*VehicleAlertCounts{}
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		rows, err := store.New(tx).AlertCountByVehicleAndType(r.Context(), store.AlertCountByVehicleAndTypeParams{From: pgtype.Timestamptz{Time: from, Valid: true}, To: pgtype.Timestamptz{Time: to, Valid: true}})
		if err != nil {
			return err
		}
		for _, row := range rows {
			if row.VehicleID == nil {
				continue // an alert with no vehicle cannot be attributed to one
			}
			id := *row.VehicleID
			code := ""
			if row.AlertCode != nil {
				code = *row.AlertCode
			}
			v := byVehicle[id]
			if v == nil {
				v = &VehicleAlertCounts{VehicleID: id, LicensePlate: row.LicensePlate, Counts: map[string]int64{}}
				byVehicle[id] = v
			}
			v.Counts[code] += row.Count
			v.Total += row.Count
			out.ByCode[code] += row.Count
			out.Total += row.Count
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "alert summary failed")
		return
	}
	// Ordered by vehicle id rather than map order, so two calls over the
	// same data return the same document.
	for _, row := range sortedVehicles(byVehicle) {
		out.Items = append(out.Items, *row)
	}
	httpx.JSON(w, http.StatusOK, out)
}

// sortedVehicles returns the tallies by ascending vehicle id.
func sortedVehicles(m map[int64]*VehicleAlertCounts) []*VehicleAlertCounts {
	ids := make([]int64, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ { // insertion sort: a fleet is hundreds, not millions
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
	out := make([]*VehicleAlertCounts, 0, len(ids))
	for _, id := range ids {
		out = append(out, m[id])
	}
	return out
}
