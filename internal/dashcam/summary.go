package dashcam

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/customreport"
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
	Types  []ClipTypeDTO  `json:"types"`
	Trucks []ClipTruckDTO `json:"trucks"`
}

// EventOutcomesDTO splits events by what became of them. Each event is in at
// most one: with video (any camera saved), no video (the camera sent none) or
// save failed (nothing saved, nothing still downloading). Events still
// downloading are in none, so the three can add up to less than events.
type EventOutcomesDTO struct {
	EventsWithVideo  int64 `json:"events_with_video"`
	EventsCameraDidntSend    int64 `json:"events_camera_didnt_send"`
	EventsSaveFailed int64 `json:"events_save_failed"`
}

// ClipTotalsDTO sums the window. Sizes are bytes of saved clips in S3.
// Clips, Failed and NoClip count files per camera; the outcomes count events.
type ClipTotalsDTO struct {
	EventOutcomesDTO
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

// ClipTypeDTO is one camera event type, most events first. Name is the same
// display name Recordings and the clip list show.
type ClipTypeDTO struct {
	EventOutcomesDTO
	Code   *int   `json:"code"`
	Name   string `json:"name"`
	Events int64  `json:"events"`
	Clips  int64  `json:"clips"`
	Bytes  int64  `json:"bytes"`
	Failed int64  `json:"failed"`
	NoClip int64  `json:"no_clip"`
}

// ClipTruckDTO is one truck. LastEvent is camera-local time, like clip_start.
type ClipTruckDTO struct {
	EventOutcomesDTO
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
//	@Description	Totals, per WIB day, per event type and per truck for automatically saved clips. Each event + camera counts once by its latest row; deleted clips are left out; still-downloading clips are left out of the sizes. events_with_video / events_camera_didnt_send / events_save_failed put each event in one outcome, however many cameras it has. Same access as the clip list.
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
	win, ok := summaryWindowOf(w, r)
	if !ok {
		return
	}
	out, err := h.summary(r.Context(), auth.EffectiveTenant(r), win)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clip summary failed")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// clipSummaryExport godoc
//
//	@Summary		Saved Evidence summary as a PDF
//	@Description	The same numbers as /v1/video-clips/summary for the same window and customer, as a PDF: overview, per event type, per truck, and what every term means. English, no AI text.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		application/pdf
//	@Param			from		query	string	true	"first day, YYYY-MM-DD"
//	@Param			to			query	string	true	"last day, YYYY-MM-DD"
//	@Param			format		query	string	false	"pdf, the only format"
//	@Param			vehicle_id	query	int		false	"one truck"
//	@Success		200	{file}		file
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/video-clips/summary/export [get]
func (h *Handler) clipSummaryExport(w http.ResponseWriter, r *http.Request) {
	if f := r.URL.Query().Get("format"); f != "" && f != string(customreport.FormatPDF) {
		httpx.Error(w, http.StatusBadRequest, "format must be pdf")
		return
	}
	win, ok := summaryWindowOf(w, r)
	if !ok {
		return
	}
	out, err := h.summary(r.Context(), auth.EffectiveTenant(r), win)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "clip summary failed")
		return
	}
	doc, err := customreport.Render(summaryDoc(out, time.Now().In(wib)), customreport.FormatPDF)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not render the document")
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	// from and to passed dayWindow's YYYY-MM-DD parse, so they cannot steer the header.
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "saved-evidence-"+out.From+"-to-"+out.To+".pdf"))
	w.Header().Set("Content-Length", strconv.Itoa(len(doc.Body)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(doc.Body)
}

// summaryWindow is a validated summary request: WIB days and an optional truck.
type summaryWindow struct {
	fromDay, toDay string
	from, to       time.Time
	vehicleID      *int64
}

// summaryWindowOf reads from, to and vehicle_id, writing a 400 when they are wrong.
func summaryWindowOf(w http.ResponseWriter, r *http.Request) (summaryWindow, bool) {
	q := r.URL.Query()
	from, to, err := dayWindow(q.Get("from"), q.Get("to"))
	if err != nil || from == nil || to == nil || !to.After(*from) {
		httpx.Error(w, http.StatusBadRequest, "from and to are required, YYYY-MM-DD, from <= to")
		return summaryWindow{}, false
	}
	win := summaryWindow{fromDay: q.Get("from"), toDay: q.Get("to"), from: *from, to: *to}
	if v := q.Get("vehicle_id"); v != "" {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "invalid vehicle_id")
			return summaryWindow{}, false
		}
		win.vehicleID = &id
	}
	return win, true
}

// summary runs the summary queries for one tenant, so the page and the PDF
// always show the same numbers.
func (h *Handler) summary(ctx context.Context, tenantID int64, win summaryWindow) (ClipSummaryDTO, error) {
	fromTs := pgtype.Timestamptz{Time: win.from, Valid: true}
	toTs := pgtype.Timestamptz{Time: win.to, Valid: true}
	vehicleID := win.vehicleID

	out := ClipSummaryDTO{From: win.fromDay, To: win.toDay, Days: []ClipDayDTO{}, Types: []ClipTypeDTO{}, Trucks: []ClipTruckDTO{}}
	err := db.Tx(ctx, h.pool, tenantID, func(tx pgx.Tx) error {
		st := store.New(tx)
		t, err := st.AutoClipSummary(ctx, store.AutoClipSummaryParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		out.Totals = totalsOf(t, int64(win.to.Sub(win.from)/(24*time.Hour)))
		days, err := st.AutoClipSummaryByDay(ctx, store.AutoClipSummaryByDayParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		for _, d := range days {
			out.Days = append(out.Days, ClipDayDTO{Day: d.Day.Time.Format("2006-01-02"), Events: d.Events,
				Clips: d.Clips, Bytes: d.Bytes, Failed: d.Failed, NoClip: d.NoClip})
		}
		types, err := st.AutoClipSummaryByType(ctx, store.AutoClipSummaryByTypeParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		for _, ty := range types {
			out.Types = append(out.Types, typeOf(ty))
		}
		trucks, err := st.AutoClipSummaryByVehicle(ctx, store.AutoClipSummaryByVehicleParams{FromTs: fromTs, ToTs: toTs, VehicleID: vehicleID})
		if err != nil {
			return err
		}
		for _, v := range trucks {
			out.Trucks = append(out.Trucks, ClipTruckDTO{VehicleID: v.VehicleID, LicensePlate: v.LicensePlate,
				Events: v.Events, Clips: v.Clips, Bytes: v.Bytes, Failed: v.Failed, NoClip: v.NoClip,
				LastEvent:        v.LastEvent.Time.UTC().Format("2006-01-02T15:04:05+00:00"),
				EventOutcomesDTO: EventOutcomesDTO{v.EventsWithVideo, v.EventsCameraDidntSend, v.EventsSaveFailed}})
		}
		return nil
	})
	return out, err
}

// typeOf keeps a row whose reason isn't "auto:<code>" under its raw reason, so
// the per-type events still add up to the totals.
func typeOf(r store.AutoClipSummaryByTypeRow) ClipTypeDTO {
	out := ClipTypeDTO{Name: r.Reason, Events: r.Events, Clips: r.Clips, Bytes: r.Bytes, Failed: r.Failed, NoClip: r.NoClip,
		EventOutcomesDTO: EventOutcomesDTO{r.EventsWithVideo, r.EventsCameraDidntSend, r.EventsSaveFailed}}
	if code, name := eventOf(r.Reason); code != nil {
		out.Code, out.Name = code, *name
	}
	return out
}

func totalsOf(t store.AutoClipSummaryRow, days int64) ClipTotalsDTO {
	out := ClipTotalsDTO{Events: t.Events, Clips: t.Clips, Bytes: t.Bytes, Failed: t.Failed,
		NoClip: t.NoClip, Pending: t.Pending, Trucks: t.Trucks,
		EventOutcomesDTO: EventOutcomesDTO{t.EventsWithVideo, t.EventsCameraDidntSend, t.EventsSaveFailed}}
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
