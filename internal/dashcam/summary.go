package dashcam

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// ClipSummaryDTO is the Saved Evidence summary of automatic saves.
type ClipSummaryDTO struct {
	From   string         `json:"from"`
	To     string         `json:"to"`
	Totals ClipTotalsDTO  `json:"totals"`
	Days   []ClipDayDTO   `json:"days"`
	Trucks []ClipTruckDTO `json:"trucks"`
}

// ClipTotalsDTO sums the window. Sizes are bytes of saved clips in S3.
type ClipTotalsDTO struct {
	Events  int64 `json:"events"`
	Clips   int64 `json:"clips"`
	Bytes   int64 `json:"bytes"`
	Failed  int64 `json:"failed"`
	NoClip  int64 `json:"no_clip"`
	Pending int64 `json:"pending"`
	Trucks  int64 `json:"trucks"`
	// Averages over the trucks with events and every day of the window.
	AvgEventsPerTruckDay float64 `json:"avg_events_per_truck_day"`
	AvgBytesPerTruckDay  float64 `json:"avg_bytes_per_truck_day"`
	AvgBytesPerClip      float64 `json:"avg_bytes_per_clip"`
}

// ClipDayDTO is one WIB day.
type ClipDayDTO struct {
	Day    string `json:"day"`
	Events int64  `json:"events"`
	Clips  int64  `json:"clips"`
	Bytes  int64  `json:"bytes"`
	Failed int64  `json:"failed"`
	NoClip int64  `json:"no_clip"`
}

// ClipTruckDTO is one truck. LastEvent is camera-local time, like clip_start.
type ClipTruckDTO struct {
	VehicleID    *int64  `json:"vehicle_id"`
	LicensePlate *string `json:"license_plate"`
	Events       int64   `json:"events"`
	Clips        int64   `json:"clips"`
	Bytes        int64   `json:"bytes"`
	Failed       int64   `json:"failed"`
	NoClip       int64   `json:"no_clip"`
	LastEvent    string  `json:"last_event"`
}

// clipSummary godoc
//
//	@Summary		Saved Evidence summary (automatic saves)
//	@Description	Totals, per WIB day and per truck for automatically saved clips. Each event + camera counts once by its latest row; deleted clips are left out; still-downloading clips are left out of the sizes. Same access as the clip list.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			from		query	string	true	"first day, YYYY-MM-DD"
//	@Param			to			query	string	true	"last day, YYYY-MM-DD"
//	@Param			vehicle_id	query	int		false	"one truck"
//	@Success		200	{object}	dashcam.ClipSummaryDTO
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/video-clips/summary [get]
func (h *Handler) clipSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, to, err := dayWindow(q.Get("from"), q.Get("to"))
	if err != nil || from == nil || to == nil || !to.After(*from) {
		httpx.Error(w, http.StatusBadRequest, "from and to are required, YYYY-MM-DD, from <= to")
		return
	}
	var vehicleID *int64
	if v := q.Get("vehicle_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
			return
		}
		vehicleID = &id
	}
	fromTs := pgtype.Timestamptz{Time: *from, Valid: true}
	toTs := pgtype.Timestamptz{Time: *to, Valid: true}

	out := ClipSummaryDTO{From: q.Get("from"), To: q.Get("to"), Days: []ClipDayDTO{}, Trucks: []ClipTruckDTO{}}
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		st := store.New(tx)
		t, err := st.AutoClipSummary(r.Context(), store.AutoClipSummaryParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		out.Totals = totalsOf(t, int64(to.Sub(*from)/(24*time.Hour)))
		days, err := st.AutoClipSummaryByDay(r.Context(), store.AutoClipSummaryByDayParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		for _, d := range days {
			out.Days = append(out.Days, ClipDayDTO{Day: d.Day.Time.Format("2006-01-02"), Events: d.Events,
				Clips: d.Clips, Bytes: d.Bytes, Failed: d.Failed, NoClip: d.NoClip})
		}
		trucks, err := st.AutoClipSummaryByVehicle(r.Context(), store.AutoClipSummaryByVehicleParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		for _, v := range trucks {
			out.Trucks = append(out.Trucks, ClipTruckDTO{VehicleID: v.VehicleID, LicensePlate: v.LicensePlate,
				Events: v.Events, Clips: v.Clips, Bytes: v.Bytes, Failed: v.Failed, NoClip: v.NoClip,
				LastEvent: v.LastEvent.Time.UTC().Format("2006-01-02T15:04:05+00:00")})
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clip summary failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func totalsOf(t store.AutoClipSummaryRow, days int64) ClipTotalsDTO {
	out := ClipTotalsDTO{Events: t.Events, Clips: t.Clips, Bytes: t.Bytes, Failed: t.Failed,
		NoClip: t.NoClip, Pending: t.Pending, Trucks: t.Trucks}
	if t.Trucks > 0 && days > 0 {
		truckDays := float64(t.Trucks * days)
		out.AvgEventsPerTruckDay = float64(t.Events) / truckDays
		out.AvgBytesPerTruckDay = float64(t.Bytes) / truckDays
	}
	if t.Clips > 0 {
		out.AvgBytesPerClip = float64(t.Bytes) / float64(t.Clips)
	}
	return out
}
