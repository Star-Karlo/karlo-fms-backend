// Package dashcam links VSS cameras to vehicles and fronts the camera UI.
//
// FMS NEVER TALKS TO VSS. The telemetry service does (it holds the VSS
// credentials, same layer as GPS ingest). This package:
//   - owns the vehicle<->device LINK and the tenant scoping (the `dashcams`
//     table), which is FMS config, not raw data
//   - calls the telemetry service for device lists, clip lists, live URLs and
//     clip downloads
//   - records each downloaded clip in `video_clips` with its S3 key, so a saved
//     accident clip is findable by truck and time
//
// The telemetry service is tenant-agnostic; this package supplies the tenant
// boundary. A user can only ask about a device that is linked to one of their
// own vehicles — so one customer cannot pull another's footage by guessing a
// device id.
package dashcam

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// DTO is a linked dashcam as the app sees it.
type DTO struct {
	ID           int64   `json:"id"`
	VehicleID    *int64  `json:"vehicle_id"`
	LicensePlate *string `json:"license_plate"`
	VehicleType  *string `json:"vehicle_type"`
	DriverName   *string `json:"driver_name"`
	VSSDeviceID  string  `json:"vss_device_id"`
	DeviceName   *string `json:"device_name"`
	Channels     int     `json:"channels"`
	ChannelNames *string `json:"channel_names"`
	Online       bool    `json:"online"`
}

// AttachRequest links a device to a vehicle.
type AttachRequest struct {
	VehicleID   int64  `json:"vehicle_id"`
	VSSDeviceID string `json:"vss_device_id"`
}

// Handler serves the FMS dashcam endpoints.
type Handler struct {
	pool *db.Pool
	tel  *telemetry.Client

	// pending clip downloads, keyed by telemetry job id — what each in-flight
	// save is for, so completion records it against the right truck.
	pmu     sync.Mutex
	pending map[string]pendingClip
}

// NewHandler builds the handler. tel is the telemetry-service client — the only
// path to VSS.
func NewHandler(pool *db.Pool, tel *telemetry.Client) *Handler {
	return &Handler{pool: pool, tel: tel}
}

// Routes mounts the endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/dashcams", h.list)
	r.Get("/v1/dashcams/available", h.available)
	r.Get("/v1/dashcams/{id}/videos", h.videos)
	r.Get("/v1/dashcams/{id}/live", h.live)
	r.Post("/v1/dashcams/{id}/download", h.download)
	r.Get("/v1/dashcams/downloads/{jobId}", h.downloadStatus)
	r.Get("/v1/video-clips", h.listClips)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleManager))
		ar.Post("/v1/dashcams", h.attach)
		ar.Delete("/v1/dashcams/{id}", h.detach)
	})
}

// list godoc
//
//	@Summary	Dashcams linked to this tenant's vehicles
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[dashcam.DTO]
//	@Router		/v1/dashcams [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var items []DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT d.id, d.vehicle_id, v.license_plate, v.vehicle_type, e.full_name,
       d.vss_device_id, d.device_name, d.channels, d.channel_names
FROM dashcams d
LEFT JOIN vehicles v  ON v.id = d.vehicle_id
LEFT JOIN employees e ON e.id = v.current_driver_id
ORDER BY v.license_plate NULLS LAST, d.vss_device_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []DTO{}
		for rows.Next() {
			var d DTO
			if err := rows.Scan(&d.ID, &d.VehicleID, &d.LicensePlate, &d.VehicleType, &d.DriverName,
				&d.VSSDeviceID, &d.DeviceName, &d.Channels, &d.ChannelNames); err != nil {
				return err
			}
			items = append(items, d)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list dashcams failed")
		return
	}
	// Overlay live device metadata from the telemetry service (best-effort): a
	// failure leaves the linked snapshot in place and everything "offline"
	// rather than failing the whole list.
	//
	// VSS is the source of truth for channel count and names, so trust its live
	// values over the snapshot taken at link time — if a device's channel config
	// changes in VSS, the picker follows without a re-link. Channels VSS reports
	// but has no camera on simply fail to stream in the grid.
	if devs, derr := h.tel.Dashcams(r.Context(), ""); derr == nil {
		byID := make(map[string]telemetry.Dashcam, len(devs))
		for _, d := range devs {
			byID[d.DeviceID] = d
		}
		for i := range items {
			d, ok := byID[items[i].VSSDeviceID]
			if !ok {
				continue // device not currently visible to VSS — keep the snapshot
			}
			items[i].Online = d.Online
			if d.Channels > 0 {
				items[i].Channels = d.Channels
			}
			if d.ChannelNames != "" {
				cn := d.ChannelNames
				items[i].ChannelNames = &cn
			}
		}
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: int64(len(items))})
}

// available godoc
//
//	@Summary		Devices the camera platform sees (install pick-list)
//	@Description	Live from the telemetry service, annotated with whether each device is already linked here.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}		map[string]interface{}
//	@Failure		502	{object}	map[string]string
//	@Router			/v1/dashcams/available [get]
func (h *Handler) available(w http.ResponseWriter, r *http.Request) {
	devices, err := h.tel.Dashcams(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	linked := map[string]string{}
	_ = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT d.vss_device_id, coalesce(v.license_plate,'') FROM dashcams d
LEFT JOIN vehicles v ON v.id = d.vehicle_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id, plate string
			if err := rows.Scan(&id, &plate); err != nil {
				return err
			}
			linked[id] = plate
		}
		return rows.Err()
	})
	type avail struct {
		telemetry.Dashcam
		LinkedTo string `json:"linked_to"`
	}
	out := make([]avail, 0, len(devices))
	for _, d := range devices {
		out = append(out, avail{Dashcam: d, LinkedTo: linked[d.DeviceID]})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// attach godoc
//
//	@Summary	Link a device to a vehicle
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dashcam.AttachRequest	true	"link"
//	@Success	201		{object}	dashcam.DTO
//	@Failure	409		{object}	map[string]string
//	@Router		/v1/dashcams [post]
func (h *Handler) attach(w http.ResponseWriter, r *http.Request) {
	var req AttachRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.VehicleID == 0 || strings.TrimSpace(req.VSSDeviceID) == "" {
		httpx.Error(w, http.StatusBadRequest, "vehicle_id and vss_device_id are required")
		return
	}
	req.VSSDeviceID = strings.TrimSpace(req.VSSDeviceID)

	// Verify the device exists on the platform before linking — otherwise a typo
	// yields a camera that lists fine and never shows a picture.
	devices, err := h.tel.Dashcams(r.Context(), req.VSSDeviceID)
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	var dev *telemetry.Dashcam
	for i := range devices {
		if devices[i].DeviceID == req.VSSDeviceID {
			dev = &devices[i]
			break
		}
	}
	if dev == nil {
		httpx.Error(w, http.StatusBadRequest, "no device with that ID on the camera platform")
		return
	}

	tid := auth.TenantID(r.Context())
	var out DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, device_name, device_type, channels, channel_names, last_sync_at)
VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''), now())
ON CONFLICT (tenant_id, vss_device_id) DO UPDATE
  SET vehicle_id=EXCLUDED.vehicle_id, device_name=EXCLUDED.device_name,
      device_type=EXCLUDED.device_type, channels=EXCLUDED.channels,
      channel_names=EXCLUDED.channel_names, last_sync_at=now(), updated_at=now()
RETURNING id, vehicle_id, vss_device_id, device_name, channels, channel_names`,
			tid, req.VehicleID, req.VSSDeviceID, dev.DeviceName, dev.DeviceType, dev.Channels, dev.ChannelNames).
			Scan(&out.ID, &out.VehicleID, &out.VSSDeviceID, &out.DeviceName, &out.Channels, &out.ChannelNames)
	})
	if err != nil {
		if strings.Contains(err.Error(), "dashcams_vehicle_uk") {
			httpx.Error(w, http.StatusConflict, "that vehicle already has a dashcam linked")
			return
		}
		log.Printf("dashcam: attach %q -> vehicle %d: %v", req.VSSDeviceID, req.VehicleID, err)
		httpx.Error(w, http.StatusBadRequest, "attach failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// detach godoc
//
//	@Summary	Unlink a dashcam
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Param		id	path	int	true	"dashcam id"
//	@Success	204
//	@Router		/v1/dashcams/{id} [delete]
func (h *Handler) detach(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), `DELETE FROM dashcams WHERE id=$1`, id)
		return e
	}); err != nil {
		httpx.Error(w, http.StatusBadRequest, "detach failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// videos godoc
//
//	@Summary	A dashcam's clips for a date, with coordinates
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id		path	int		true	"dashcam id"
//	@Param		date	query	string	true	"YYYY-MM-DD"
//	@Success	200		{array}	telemetry.DashcamClip
//	@Router		/v1/dashcams/{id}/videos [get]
func (h *Handler) videos(w http.ResponseWriter, r *http.Request) {
	dc, ok := h.resolve(w, r)
	if !ok {
		return
	}
	clips, err := h.tel.DashcamVideos(r.Context(), dc.deviceID, r.URL.Query().Get("date"), r.URL.Query().Get("channel"))
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, clips)
}

// live godoc
//
//	@Summary	Live-view URL for a dashcam
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path	int	true	"dashcam id"
//	@Success	200	{object}	map[string]string
//	@Router		/v1/dashcams/{id}/live [get]
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	dc, ok := h.resolve(w, r)
	if !ok {
		return
	}
	u, err := h.tel.DashcamLiveURL(r.Context(), dc.deviceID, r.URL.Query().Get("channel"), r.URL.Query().Get("stream"))
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"url": u})
}

// DownloadRequest asks for a clip to be saved to S3.
type DownloadRequest struct {
	Channel int      `json:"channel"`
	Path    string   `json:"path"`
	Start   string   `json:"start"`
	Stop    string   `json:"stop"`
	Lat     *float64 `json:"lat"`
	Lon     *float64 `json:"lon"`
	Note    string   `json:"note"`
}

// download godoc
//
//	@Summary		Save a clip to S3 (async)
//	@Description	Asks the telemetry service to pull the clip to S3; returns a job id to poll. Poll /v1/dashcams/downloads/{jobId}; on completion FMS records the clip in video_clips.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path	int						true	"dashcam id"
//	@Param			body	body	dashcam.DownloadRequest	true	"clip"
//	@Success		202		{object}	map[string]string
//	@Router			/v1/dashcams/{id}/download [post]
func (h *Handler) download(w http.ResponseWriter, r *http.Request) {
	dc, ok := h.resolve(w, r)
	if !ok {
		return
	}
	var req DownloadRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Start == "" || req.Stop == "" {
		// Downloads are by time range now (path unused), so the window is what's
		// required — works for SD-card clips and alarm-derived clips alike.
		httpx.Error(w, http.StatusBadRequest, "start and stop are required")
		return
	}
	jobID, err := h.tel.StartDashcamDownload(r.Context(), dc.deviceID, telemetry.DashcamDownloadReq{
		Channel: req.Channel, Path: req.Path, Start: req.Start, Stop: req.Stop,
	})
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	// Remember what this job is for, so when it completes we can record the clip
	// against the right truck without the client having to send it all back.
	h.remember(jobID, pendingClip{
		tenantID: auth.TenantID(r.Context()), dashcamID: dc.id, vehicleID: dc.vehicleID,
		deviceID: dc.deviceID, channel: req.Channel, start: req.Start, stop: req.Stop,
		lat: req.Lat, lon: req.Lon, note: req.Note, userID: auth.UserID(r.Context()),
	})
	httpx.JSON(w, http.StatusAccepted, map[string]string{"job_id": jobID})
}

// downloadStatus godoc
//
//	@Summary		Poll a save-to-S3 job
//	@Description	Relays the telemetry job status. On "done" the clip is recorded in video_clips and its id is returned.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			jobId	path	string	true	"job id"
//	@Success		200		{object}	map[string]interface{}
//	@Router			/v1/dashcams/downloads/{jobId} [get]
func (h *Handler) downloadStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobId")
	st, err := h.tel.DashcamDownloadStatus(r.Context(), jobID)
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	resp := map[string]any{"status": st.Status}
	if st.Error != "" {
		resp["error"] = st.Error
	}
	// On completion, record the clip once (the pending entry is consumed) so a
	// repeat poll does not insert twice.
	if st.Status == "done" && st.S3Key != "" {
		if pc, ok := h.take(jobID); ok {
			clipID, cerr := h.recordClip(r.Context(), pc, st.Bucket, st.S3Key)
			if cerr != nil {
				log.Printf("dashcam: record clip for job %s: %v", jobID, cerr)
			} else {
				resp["clip_id"] = clipID
			}
		}
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// listClips godoc
//
//	@Summary	Saved clips (accident evidence)
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Param		vehicle_id	query	int	false	"filter by vehicle"
//	@Success	200	{object}	httpx.List[dashcam.ClipDTO]
//	@Router		/v1/video-clips [get]
func (h *Handler) listClips(w http.ResponseWriter, r *http.Request) {
	var vehicleFilter *int64
	if v := r.URL.Query().Get("vehicle_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			vehicleFilter = &id
		}
	}
	var items []ClipDTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT c.id, c.vehicle_id, v.license_plate, c.vss_device_id, c.channel,
       to_char(c.clip_start, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       c.s3_bucket, c.s3_key, c.size_bytes, c.lat, c.lon, c.reason, c.note,
       to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00')
FROM video_clips c LEFT JOIN vehicles v ON v.id = c.vehicle_id
WHERE ($1::bigint IS NULL OR c.vehicle_id = $1)
ORDER BY c.clip_start DESC LIMIT 500`, vehicleFilter)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []ClipDTO{}
		for rows.Next() {
			var c ClipDTO
			if err := rows.Scan(&c.ID, &c.VehicleID, &c.LicensePlate, &c.VSSDeviceID, &c.Channel,
				&c.ClipStart, &c.S3Bucket, &c.S3Key, &c.SizeBytes, &c.Lat, &c.Lon, &c.Reason, &c.Note, &c.CreatedAt); err != nil {
				return err
			}
			items = append(items, c)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list clips failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[ClipDTO]{Items: items, Total: int64(len(items))})
}

// ClipDTO is a saved clip.
type ClipDTO struct {
	ID           int64    `json:"id"`
	VehicleID    *int64   `json:"vehicle_id"`
	LicensePlate *string  `json:"license_plate"`
	VSSDeviceID  string   `json:"vss_device_id"`
	Channel      int      `json:"channel"`
	ClipStart    string   `json:"clip_start"`
	S3Bucket     string   `json:"s3_bucket"`
	S3Key        string   `json:"s3_key"`
	SizeBytes    *int64   `json:"size_bytes"`
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	Reason       string   `json:"reason"`
	Note         *string  `json:"note"`
	CreatedAt    string   `json:"created_at"`
}

// resolved is the tenant-scoped device behind a dashcam id.
type resolved struct {
	id        int64
	deviceID  string
	vehicleID *int64
}

// resolve loads a dashcam scoped to the caller's tenant — so a user can only act
// on a camera linked to their own fleet.
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) (resolved, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return resolved{}, false
	}
	var out resolved
	out.id = id
	if err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`SELECT vss_device_id, vehicle_id FROM dashcams WHERE id=$1`, id).Scan(&out.deviceID, &out.vehicleID)
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "dashcam not found")
		return resolved{}, false
	}
	return out, true
}

func (h *Handler) gatewayError(w http.ResponseWriter, err error) {
	// The telemetry service returns 503 when VSS is unconfigured; surface that
	// verbatim so the operator sees the real cause.
	if strings.Contains(err.Error(), "503") {
		httpx.Error(w, http.StatusServiceUnavailable, "camera platform not configured yet")
		return
	}
	log.Printf("dashcam: telemetry call: %v", err)
	httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
}
