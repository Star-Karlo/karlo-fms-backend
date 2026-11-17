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
	"context"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
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
	// IsDemo: a copy of another account's camera, shown here for a demo.
	IsDemo bool `json:"is_demo"`
	Online bool `json:"online"`
}

// AttachRequest links a device to a vehicle.
type AttachRequest struct {
	VehicleID   int64  `json:"vehicle_id"`
	VSSDeviceID string `json:"vss_device_id"`
	// IsDemo: this account only demos the camera; another account owns it.
	// Not registered in master data.
	IsDemo bool `json:"is_demo"`
}

// Handler serves the FMS dashcam endpoints.
type Handler struct {
	pool    *db.Pool
	tel     *telemetry.Client
	presign *s3.PresignClient       // signs GET URLs for saved clips (nil = disabled)
	s3c     *s3.Client              // deletes the S3 object when a saved clip is removed
	md      *mdwrite.Client         // nil before cutover: links stay local
	dir     mdwrite.CompanyResolver // tenant -> company for the write-through's caller
	buckets map[int64]string        // tenant -> clip bucket; missing = telemetry's default
}

// WithTenantBuckets sends each listed customer's clips to its own bucket.
func (h *Handler) WithTenantBuckets(b map[int64]string) *Handler {
	h.buckets = b
	return h
}

// WithMasterData makes a link/unlink reach master data (kind=dashcam,
// fit/unfit) — it owns devices; trackersync projects them back.
func (h *Handler) WithMasterData(md *mdwrite.Client, dir mdwrite.CompanyResolver) *Handler {
	h.md, h.dir = md, dir
	return h
}

// mdCaller builds the identity to forward upstream (409 when a staff member
// acts for a tenant whose company is unknown).
func (h *Handler) mdCaller(w http.ResponseWriter, r *http.Request) (mdwrite.Caller, bool) {
	if h.md == nil {
		return mdwrite.Caller{}, true
	}
	c, err := mdwrite.CallerFor(r, auth.TenantID(r.Context()), auth.EffectiveTenant(r), h.dir)
	if err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return c, false
	}
	return c, true
}

// mirrorLink tells master data a dashcam is on a vehicle: the device is
// registered there on first sight (kind=dashcam, device id = VSS id), then
// fitted. The local row keeps the master-data id so a later unlink can
// unfit the same record. Failures are logged, not fatal: the VSS platform
// is the camera's real registry and the link here already works.
func (h *Handler) mirrorLink(ctx context.Context, caller mdwrite.Caller, tid, dashcamID int64, vssID string, vehicleID int64) {
	if h.md == nil {
		return
	}
	var mdID, vehicleMD *string
	var demo bool
	_ = db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT d.master_data_id, d.is_demo, v.master_data_id FROM dashcams d JOIN vehicles v ON v.id = d.vehicle_id WHERE d.id = $1`, dashcamID).Scan(&mdID, &demo, &vehicleMD)
	})
	if demo {
		return // a demo copy of another account's camera: master data keeps the real owner
	}
	if mdID == nil {
		id, err := h.md.CreateTracker(ctx, caller, mdwrite.Tracker{Kind: "dashcam", DeviceID: vssID, Status: "active"})
		if err != nil {
			log.Printf("dashcam %d: linked locally, master data refused the device: %v", dashcamID, err)
			return
		}
		mdID = &id
		_ = db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE dashcams SET master_data_id = $2 WHERE id = $1 AND master_data_id IS NULL`, dashcamID, id)
			return err
		})
	}
	if vehicleMD == nil {
		log.Printf("dashcam %d: vehicle %d has no master_data_id; master data not told of the fitting", dashcamID, vehicleID)
		return
	}
	if err := h.md.FitTracker(ctx, caller, *mdID, *vehicleMD); err != nil {
		log.Printf("dashcam %d: linked locally, master data %s rejected the fit: %v", dashcamID, *mdID, err)
	}
}

// NewHandler builds the handler. tel is the telemetry-service client — the only
// path to VSS. region enables presigned view/download URLs for saved clips.
func NewHandler(ctx context.Context, pool *db.Pool, tel *telemetry.Client, region string) *Handler {
	h := &Handler{pool: pool, tel: tel}
	if cfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(region)); err == nil {
		c := s3.NewFromConfig(cfg)
		h.s3c = c
		h.presign = s3.NewPresignClient(c)
	}
	return h
}

// Routes mounts the endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/dashcams", h.list)
	r.Get("/v1/dashcams/available", h.available)
	r.Get("/v1/dashcams/{id}/videos", h.videos)
	r.Get("/v1/dashcams/{id}/evidence", h.evidence)
	r.Get("/v1/dashcams/{id}/live", h.live)
	r.Post("/v1/dashcams/{id}/download", h.download)
	r.Get("/v1/dashcams/{id}/clip", h.findClip)
	r.Get("/v1/dashcams/downloads/{jobId}", h.downloadStatus)
	r.Get("/v1/video-clips", h.listClips)
	r.Get("/v1/video-clips/summary", h.clipSummary)
	r.Get("/v1/video-clips/{id}/url", h.clipURL)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleManager))
		ar.Post("/v1/dashcams", h.attach)
		ar.Delete("/v1/dashcams/{id}", h.detach)
		ar.Delete("/v1/video-clips/{id}", h.deleteClip)
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
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT d.id, d.vehicle_id, v.license_plate, v.vehicle_type, e.full_name,
       d.vss_device_id, d.device_name, d.channels, d.channel_names, d.is_demo
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
				&d.VSSDeviceID, &d.DeviceName, &d.Channels, &d.ChannelNames, &d.IsDemo); err != nil {
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
	_ = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
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

	tid := auth.EffectiveTenant(r)
	caller, ok := h.mdCaller(w, r)
	if !ok {
		return
	}
	var out DTO
	err = db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `
INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, device_name, device_type, channels, channel_names, last_sync_at, is_demo)
VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''), now(), $8)
ON CONFLICT (tenant_id, vss_device_id) DO UPDATE
  SET vehicle_id=EXCLUDED.vehicle_id, device_name=EXCLUDED.device_name,
      device_type=EXCLUDED.device_type, channels=EXCLUDED.channels,
      channel_names=EXCLUDED.channel_names, last_sync_at=now(), updated_at=now(),
      is_demo=dashcams.is_demo OR EXCLUDED.is_demo
RETURNING id, vehicle_id, vss_device_id, device_name, channels, channel_names`,
			tid, req.VehicleID, req.VSSDeviceID, dev.DeviceName, dev.DeviceType, dev.Channels, dev.ChannelNames, req.IsDemo).
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
	h.mirrorLink(r.Context(), caller, tid, out.ID, req.VSSDeviceID, req.VehicleID)
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
	caller, ok := h.mdCaller(w, r)
	if !ok {
		return
	}
	var mdID *string
	if err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		_ = tx.QueryRow(r.Context(), `SELECT master_data_id FROM dashcams WHERE id=$1`, id).Scan(&mdID)
		_, e := tx.Exec(r.Context(), `DELETE FROM dashcams WHERE id=$1`, id)
		return e
	}); err != nil {
		httpx.Error(w, http.StatusBadRequest, "detach failed")
		return
	}
	// The device stays registered upstream; only its fitting closes.
	if h.md != nil && mdID != nil {
		if err := h.md.UnfitTracker(r.Context(), caller, *mdID); err != nil {
			log.Printf("dashcam %d: unlinked locally, master data %s rejected the unfit: %v", id, *mdID, err)
		}
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

// clipURL godoc
//
//	@Summary		A short-lived URL to view or download a saved clip
//	@Description	Presigned S3 GET URL. ?download=1 forces a file download; else it
//	@Description	plays inline in the browser.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id			path	int		true	"clip id"
//	@Param			download	query	bool	false	"force download"
//	@Success		200	{object}	map[string]string
//	@Router			/v1/video-clips/{id}/url [get]
func (h *Handler) clipURL(w http.ResponseWriter, r *http.Request) {
	if h.presign == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "clip storage is not configured")
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var bucket, key *string
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		// RLS scopes this: another tenant's id returns no rows -> 404.
		return tx.QueryRow(r.Context(),
			`SELECT s3_bucket, s3_key FROM video_clips WHERE id=$1 AND status='ready'`, id).Scan(&bucket, &key)
	})
	if err != nil || key == nil || *key == "" {
		httpx.Error(w, http.StatusNotFound, "clip not found or not ready")
		return
	}
	in := &s3.GetObjectInput{Bucket: bucket, Key: key, ResponseContentType: awsString("video/mp4")}
	if r.URL.Query().Get("download") != "" {
		in.ResponseContentDisposition = awsString(`attachment; filename="clip.mp4"`)
	}
	signed, err := h.presign.PresignGetObject(r.Context(), in, s3.WithPresignExpires(10*time.Minute))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "could not create URL")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"url": signed.URL, "expires_in_seconds": 600})
}

func awsString(s string) *string { return &s }

var errClipDownloading = errors.New("clip still downloading")

// deleteClip removes a saved evidence clip: the S3 object (best-effort) and the
// video_clips row. RLS scopes it to the caller's tenant (EffectiveTenant lets a
// platform admin target ?account_id), so one tenant can't delete another's clip.
//
// An auto-saved clip keeps its row as 'deleted': without it the next auto-save
// run would find no row and save the clip again. A clip still downloading
// can't be deleted, since telemetry's job would still write the S3 object.
//
//	@Summary		Delete a saved evidence clip
//	@Description	Removes the S3 object, and the video_clips record (an auto-saved clip keeps it as 'deleted' so it isn't saved again). 409 while the clip is still downloading. Manager+.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Param			id	path	int	true	"clip id"
//	@Success		204
//	@Failure		404	{object}	map[string]string
//	@Failure		409	{object}	map[string]string
//	@Router			/v1/video-clips/{id} [delete]
func (h *Handler) deleteClip(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var bucket, key, alarmID *string
	var status string
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		// Read the object location, then delete the row — both under RLS, so a
		// missing/other-tenant id affects nothing and 404s.
		if e := tx.QueryRow(r.Context(),
			`SELECT s3_bucket, s3_key, status, alarm_id FROM video_clips WHERE id=$1 AND status <> 'deleted'`,
			id).Scan(&bucket, &key, &status, &alarmID); e != nil {
			return e
		}
		if status == "pending" {
			return errClipDownloading
		}
		q := `DELETE FROM video_clips WHERE id=$1`
		if alarmID != nil {
			q = `UPDATE video_clips SET status='deleted', s3_bucket=NULL, s3_key=NULL, size_bytes=NULL, error=NULL WHERE id=$1`
		}
		tag, e := tx.Exec(r.Context(), q, id)
		if e != nil {
			return e
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	if errors.Is(err, errClipDownloading) {
		httpx.Error(w, http.StatusConflict, "Wait until the download finishes.")
		return
	}
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "clip not found")
		return
	}
	// Best-effort S3 removal — the row is already gone, so a delete failure (e.g.
	// missing s3:DeleteObject permission) just leaves the object for the bucket's
	// lifecycle to expire rather than failing the user's action.
	if h.s3c != nil && bucket != nil && key != nil && *key != "" {
		if _, derr := h.s3c.DeleteObject(r.Context(), &s3.DeleteObjectInput{Bucket: bucket, Key: key}); derr != nil {
			log.Printf("dashcam: clip %d row deleted but S3 object %q not removed: %v", id, *key, derr)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// EvidenceDTO is one alarm-evidence clip, named for the UI.
type EvidenceDTO struct {
	Code     int       `json:"code"`
	Name     string    `json:"name"`
	Severity string    `json:"severity"`
	Time     time.Time `json:"time"`
	Lat      *float64  `json:"lat"`
	Lon      *float64  `json:"lon"`
	Channel  int       `json:"channel"`
	// AlarmID matches the event to its auto-saved clip (video_clips.alarm_id).
	AlarmID string `json:"alarm_id,omitempty"`
}

// evidence lists a camera's alarm-evidence clips for a date — the Recordings
// source, with the REAL alarm time and a name, so every row saves fast from the
// pre-uploaded clip.
func (h *Handler) evidence(w http.ResponseWriter, r *http.Request) {
	dc, ok := h.resolve(w, r)
	if !ok {
		return
	}
	evs, err := h.tel.DashcamEvidence(r.Context(), dc.deviceID, r.URL.Query().Get("date"))
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	out := make([]EvidenceDTO, 0, len(evs))
	for _, e := range evs {
		code, name, sev := alarmMeta(e.Code)
		_ = code
		out = append(out, EvidenceDTO{Code: e.Code, Name: name, Severity: sev, Time: e.Time, Lat: e.Lat, Lon: e.Lon, Channel: e.Channel, AlarmID: e.AlarmID})
	}
	httpx.JSON(w, http.StatusOK, httpx.List[EvidenceDTO]{Items: out, Total: int64(len(out))})
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
	// Idempotent: the same window on the same camera is one clip. A second
	// Save — a re-click, a refreshed page, another operator — returns the
	// clip already pending or ready instead of pulling the footage again.
	// Only a FAILED clip is retried.
	if existing, err := h.lookupClip(r.Context(), auth.EffectiveTenant(r), dc.deviceID, req.Channel, req.Start); err != nil {
		log.Printf("dashcam: lookup clip before download: %v", err)
	} else if existing != nil && existing.Status != "failed" {
		httpx.JSON(w, http.StatusOK, map[string]any{"job_id": existing.JobID, "clip_id": existing.ID, "status": existing.Status, "existing": true})
		return
	}
	jobID, err := h.tel.StartDashcamDownload(r.Context(), dc.deviceID, telemetry.DashcamDownloadReq{
		Channel: req.Channel, Path: req.Path, Start: req.Start, Stop: req.Stop,
		Bucket: h.buckets[auth.EffectiveTenant(r)],
	})
	if err != nil {
		h.gatewayError(w, err)
		return
	}
	// Record the clip as PENDING right away, so it appears in Saved Evidence the
	// instant the operator clicks Save. The reconciler flips it to ready/failed
	// when the background download finishes — the client need not wait or poll.
	pc := pendingClip{
		tenantID: auth.EffectiveTenant(r), dashcamID: dc.id, vehicleID: dc.vehicleID,
		deviceID: dc.deviceID, channel: req.Channel, start: req.Start, stop: req.Stop,
		lat: req.Lat, lon: req.Lon, note: req.Note, userID: auth.UserID(r.Context()),
	}
	clipID, err := h.insertPendingClip(r.Context(), pc, jobID)
	if err != nil {
		log.Printf("dashcam: insert pending clip for job %s: %v", jobID, err)
		httpx.Error(w, http.StatusInternalServerError, "could not start the save")
		return
	}
	httpx.JSON(w, http.StatusAccepted, map[string]any{"job_id": jobID, "clip_id": clipID, "status": "pending", "existing": false})
}

// findClip godoc
//
//	@Summary		The saved clip for a camera + window, if any
//	@Description	Lets a client that lost its state (a refreshed page) find the clip a previous Save produced — pending, ready or failed — instead of saving again. Same start string as the download request.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id		path	int		true	"dashcam id"
//	@Param			channel	query	int		true	"camera channel"
//	@Param			start	query	string	true	"clip start, YYYY-MM-DD HH:MM:SS (camera-local, as sent to download)"
//	@Success		200		{object}	dashcam.ClipDTO
//	@Failure		404		{object}	map[string]string
//	@Router			/v1/dashcams/{id}/clip [get]
func (h *Handler) findClip(w http.ResponseWriter, r *http.Request) {
	dc, ok := h.resolve(w, r)
	if !ok {
		return
	}
	ch, _ := strconv.Atoi(r.URL.Query().Get("channel"))
	start := r.URL.Query().Get("start")
	if start == "" {
		httpx.Error(w, http.StatusBadRequest, "start is required")
		return
	}
	c, err := h.lookupClip(r.Context(), auth.EffectiveTenant(r), dc.deviceID, ch, start)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if c == nil {
		httpx.Error(w, http.StatusNotFound, "no saved clip for this window")
		return
	}
	httpx.JSON(w, http.StatusOK, c)
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
	// The clip row is recorded and completed server-side (insertPendingClip +
	// the reconciler), so this just relays the live job status for any caller
	// that wants immediate feedback.
	resp := map[string]any{"status": st.Status}
	if st.Error != "" {
		resp["error"] = st.Error
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// listClips godoc
//
//	@Summary		Saved clips (accident evidence)
//	@Description	Newest first. Deleted auto-saved clips are hidden unless status=deleted. from/to are WIB days (YYYY-MM-DD) on the clip time. Without limit it returns up to 500, as before.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			vehicle_id	query	int		false	"filter by vehicle"
//	@Param			from		query	string	false	"first day, YYYY-MM-DD"
//	@Param			to			query	string	false	"last day, YYYY-MM-DD"
//	@Param			status		query	string	false	"pending | ready | failed | no_clip | deleted"
//	@Param			limit		query	int		false	"page size (max 500)"
//	@Param			offset		query	int		false	"rows to skip"
//	@Success		200	{object}	httpx.List[dashcam.ClipDTO]
//	@Failure		400	{object}	map[string]string
//	@Router			/v1/video-clips [get]
func (h *Handler) listClips(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var vehicleFilter *int64
	if v := q.Get("vehicle_id"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			vehicleFilter = &id
		}
	}
	from, to, err := dayWindow(q.Get("from"), q.Get("to"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	p := httpx.ParseList(r)
	if q.Get("limit") == "" {
		p.Limit = 500
	}
	var status *string
	if p.Status != "" {
		status = &p.Status
	}
	const where = `
WHERE ($1::bigint IS NULL OR c.vehicle_id = $1)
  AND ($2::timestamptz IS NULL OR c.clip_start >= $2)
  AND ($3::timestamptz IS NULL OR c.clip_start < $3)
  AND (($4::text IS NULL AND c.status <> 'deleted') OR c.status = $4)`
	var items []ClipDTO
	var total int64
	err = db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM video_clips c`+where,
			vehicleFilter, from, to, status).Scan(&total); err != nil {
			return err
		}
		rows, err := tx.Query(r.Context(), `
SELECT c.id, c.vehicle_id, v.license_plate, c.vss_device_id, c.channel,
       to_char(c.clip_start, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       c.status, c.error, c.s3_bucket, c.s3_key, c.size_bytes, c.lat, c.lon, c.reason, c.note,
       to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'), c.alarm_id
FROM video_clips c LEFT JOIN vehicles v ON v.id = c.vehicle_id`+where+`
ORDER BY c.created_at DESC, c.id DESC LIMIT $5 OFFSET $6`, vehicleFilter, from, to, status, p.Limit, p.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []ClipDTO{}
		for rows.Next() {
			var c ClipDTO
			if err := rows.Scan(&c.ID, &c.VehicleID, &c.LicensePlate, &c.VSSDeviceID, &c.Channel,
				&c.ClipStart, &c.Status, &c.Error, &c.S3Bucket, &c.S3Key, &c.SizeBytes, &c.Lat, &c.Lon, &c.Reason, &c.Note, &c.CreatedAt, &c.AlarmID); err != nil {
				return err
			}
			c.EventCode, c.EventName = eventOf(c.Reason)
			items = append(items, c)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list clips failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[ClipDTO]{Items: items, Total: total})
}

// eventOf reads the camera event an auto-saved clip was kept for from its
// reason ("auto:124"). Manual clips have none.
func eventOf(reason string) (*int, *string) {
	code, err := strconv.Atoi(strings.TrimPrefix(reason, "auto:"))
	if !strings.HasPrefix(reason, "auto:") || err != nil {
		return nil, nil
	}
	_, name, _ := alarmMeta(code)
	return &code, &name
}

// dayWindow turns optional WIB days into clip_start bounds. clip_start holds
// WIB clock time labelled UTC, so a WIB day starts at that date's UTC midnight.
func dayWindow(fromDay, toDay string) (from, to *time.Time, err error) {
	if fromDay != "" {
		t, e := time.Parse("2006-01-02", fromDay)
		if e != nil {
			return nil, nil, errors.New("from must be YYYY-MM-DD")
		}
		from = &t
	}
	if toDay != "" {
		t, e := time.Parse("2006-01-02", toDay)
		if e != nil {
			return nil, nil, errors.New("to must be YYYY-MM-DD")
		}
		end := t.AddDate(0, 0, 1)
		to = &end
	}
	return from, to, nil
}

// ClipDTO is a saved clip.
type ClipDTO struct {
	ID           int64   `json:"id"`
	VehicleID    *int64  `json:"vehicle_id"`
	LicensePlate *string `json:"license_plate"`
	VSSDeviceID  string  `json:"vss_device_id"`
	Channel      int     `json:"channel"`
	ClipStart    string  `json:"clip_start"`
	// pending | ready | failed | no_clip | deleted. s3_key/bucket are null until ready.
	Status    string   `json:"status"`
	Error     *string  `json:"error"`
	S3Bucket  *string  `json:"s3_bucket"`
	S3Key     *string  `json:"s3_key"`
	SizeBytes *int64   `json:"size_bytes"`
	Lat       *float64 `json:"lat"`
	Lon       *float64 `json:"lon"`
	Reason    string   `json:"reason"`
	Note      *string  `json:"note"`
	CreatedAt string   `json:"created_at"`
	// AlarmID is the camera event an auto-saved clip belongs to; EventCode and
	// EventName name it. All nil for manual saves.
	AlarmID   *string `json:"alarm_id"`
	EventCode *int    `json:"event_code"`
	EventName *string `json:"event_name"`
	// JobID of the telemetry download, for polling live stage while pending. Empty on list.
	JobID string `json:"job_id,omitempty"`
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
	if err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
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
