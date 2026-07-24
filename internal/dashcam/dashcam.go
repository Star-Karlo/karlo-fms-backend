// Package dashcam links Gtrack VSS cameras to vehicles and hands out the
// short-lived URLs the browser needs to watch them.
//
// THE PIPELINE, which mirrors telemetry:
//
//	MDVR --3G/4G--> VSS platform --HTTP poll--> this package --> dashcams table
//	                (owns the video)             joins on VSS deviceID
//
// Video never lands in this database. VSS stores it; FMS stores the link plus
// the cached state a screen needs before opening a stream (online, last seen,
// channel count). Watching goes browser -> VSS directly, the way map tiles do
// not pass through this API.
//
// WHY /live RETURNS A URL RATHER THAN PROXYING. The vendor ships an iframe
// player and its token travels in the query string; there is no server-side
// streaming contract to proxy. The token is therefore minted PER VIEW and never
// persisted client-side. It still carries whatever the VSS account can do — on
// an admin account that includes apiVehicleControl, whose actions 6 and 8 cut a
// truck's oil and electrical circuits. Pointing VSS_USERNAME at a view-only VSS
// user removes that exposure and changes nothing else here.
package dashcam

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/vss"
)

// DTO is a dashcam as the app sees it.
type DTO struct {
	ID           int64   `json:"id"`
	VehicleID    *int64  `json:"vehicle_id"`
	LicensePlate *string `json:"license_plate"`
	VSSDeviceID  string  `json:"vss_device_id"`
	DeviceName   *string `json:"device_name"`
	DeviceType   *string `json:"device_type"`
	Channels     int     `json:"channels"`
	ChannelNames *string `json:"channel_names"`
	AccessMode   *int    `json:"access_mode"`
	Online       bool    `json:"online"`
	LastOnlineAt *string `json:"last_online_at"`
	LastSyncAt   *string `json:"last_sync_at"`
	Status       string  `json:"status"`
}

// AttachRequest links a VSS device to a vehicle.
type AttachRequest struct {
	VehicleID   int64  `json:"vehicle_id"`
	VSSDeviceID string `json:"vss_device_id"`
}

// Handler exposes dashcam endpoints.
type Handler struct {
	pool *db.Pool
	vss  *vss.Client
}

// NewHandler builds the handler. A nil-configured client is fine — the
// endpoints answer 503 with an actionable message rather than failing oddly.
func NewHandler(pool *db.Pool, client *vss.Client) *Handler {
	return &Handler{pool: pool, vss: client}
}

// Routes mounts the dashcam endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/dashcams", h.list)
	r.Get("/v1/dashcams/available", h.available)
	r.Get("/v1/dashcams/{id}/live", h.live)
	r.Get("/v1/dashcams/{id}/playback", h.playback)
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleManager))
		ar.Post("/v1/dashcams", h.attach)
		ar.Delete("/v1/dashcams/{id}", h.detach)
		ar.Post("/v1/dashcams/sync", h.sync)
	})
}

func (h *Handler) unavailable(w http.ResponseWriter, err error) bool {
	if errors.Is(err, vss.ErrNotConfigured) {
		httpx.Error(w, http.StatusServiceUnavailable,
			"camera platform not configured — set VSS_BASE_URL, VSS_USERNAME and VSS_PASSWORD")
		return true
	}
	return false
}

// list godoc
//
//	@Summary	List dashcams linked to this tenant's vehicles
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[dashcam.DTO]
//	@Router		/v1/dashcams [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var items []DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT d.id, d.vehicle_id, v.license_plate, d.vss_device_id, d.device_name, d.device_type,
       d.channels, d.channel_names, d.access_mode,
       to_char(d.last_online_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       to_char(d.last_sync_at,   'YYYY-MM-DD"T"HH24:MI:SSOF:00'), d.status
FROM dashcams d LEFT JOIN vehicles v ON v.id = d.vehicle_id
ORDER BY v.license_plate NULLS LAST, d.vss_device_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []DTO{}
		for rows.Next() {
			var d DTO
			if err := rows.Scan(&d.ID, &d.VehicleID, &d.LicensePlate, &d.VSSDeviceID, &d.DeviceName,
				&d.DeviceType, &d.Channels, &d.ChannelNames, &d.AccessMode,
				&d.LastOnlineAt, &d.LastSyncAt, &d.Status); err != nil {
				return err
			}
			d.Online = d.AccessMode != nil && *d.AccessMode > 0
			items = append(items, d)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list dashcams failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: int64(len(items))})
}

// available godoc
//
//	@Summary		Devices known to VSS, for the install flow
//	@Description	Live from the camera platform, annotated with whether each device is already linked here. This is what makes installation a pick-list rather than typing device IDs.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{array}	map[string]interface{}
//	@Failure		503	{object}	map[string]string
//	@Router			/v1/dashcams/available [get]
func (h *Handler) available(w http.ResponseWriter, r *http.Request) {
	devices, err := h.vss.ListDevices(r.Context(), strings.TrimSpace(r.URL.Query().Get("q")))
	if err != nil {
		if h.unavailable(w, err) {
			return
		}
		log.Printf("dashcam: list vss devices: %v", err)
		httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
		return
	}
	// Which are already linked, so the installer is not offered a device that
	// is on another truck — the mistake that puts one truck's camera under
	// another truck's plate.
	linked := map[string]string{}
	_ = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT d.vss_device_id, coalesce(v.license_plate, '') FROM dashcams d
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
		VSSDeviceID  string `json:"vss_device_id"`
		DeviceName   string `json:"device_name"`
		DeviceType   string `json:"device_type"`
		Channels     int    `json:"channels"`
		ChannelNames string `json:"channel_names"`
		Online       bool   `json:"online"`
		AccessMode   int    `json:"access_mode"`
		LinkedTo     string `json:"linked_to"`
	}
	out := make([]avail, 0, len(devices))
	for _, d := range devices {
		out = append(out, avail{
			VSSDeviceID: d.DeviceNo, DeviceName: d.DeviceName, DeviceType: d.DeviceType,
			Channels: d.VideoChannels, ChannelNames: d.ChannelName,
			Online: d.Online(), AccessMode: d.AccessMode, LinkedTo: linked[d.DeviceNo],
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// attach godoc
//
//	@Summary	Link a VSS device to a vehicle
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

	// Confirm the device exists on the platform BEFORE storing the link. A typo
	// otherwise produces a camera that is registered, looks fine in the list,
	// and simply never shows a picture — with nothing saying why.
	var dev *vss.Device
	devices, err := h.vss.ListDevices(r.Context(), req.VSSDeviceID)
	if err != nil {
		if h.unavailable(w, err) {
			return
		}
		log.Printf("dashcam: verify device %q: %v", req.VSSDeviceID, err)
		httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
		return
	}
	for i := range devices {
		if devices[i].DeviceNo == req.VSSDeviceID {
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
INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, device_name, device_type,
                      channels, channel_names, access_mode, last_sync_at)
VALUES ($1,$2,$3,NULLIF($4,''),NULLIF($5,''),$6,NULLIF($7,''),$8, now())
ON CONFLICT (tenant_id, vss_device_id) DO UPDATE
  SET vehicle_id=EXCLUDED.vehicle_id, device_name=EXCLUDED.device_name,
      device_type=EXCLUDED.device_type, channels=EXCLUDED.channels,
      channel_names=EXCLUDED.channel_names, access_mode=EXCLUDED.access_mode,
      last_sync_at=now(), updated_at=now()
RETURNING id, vehicle_id, vss_device_id, device_name, device_type, channels,
          channel_names, access_mode, status`,
			tid, req.VehicleID, req.VSSDeviceID, dev.DeviceName, dev.DeviceType,
			dev.VideoChannels, dev.ChannelName, dev.AccessMode).
			Scan(&out.ID, &out.VehicleID, &out.VSSDeviceID, &out.DeviceName, &out.DeviceType,
				&out.Channels, &out.ChannelNames, &out.AccessMode, &out.Status)
	})
	if err != nil {
		// A truck already has a camera, or this camera is on another truck.
		if strings.Contains(err.Error(), "dashcams_vehicle_uk") {
			httpx.Error(w, http.StatusConflict, "that vehicle already has a dashcam linked")
			return
		}
		log.Printf("dashcam: attach %q to vehicle %d: %v", req.VSSDeviceID, req.VehicleID, err)
		httpx.Error(w, http.StatusBadRequest, "attach failed")
		return
	}
	out.Online = out.AccessMode != nil && *out.AccessMode > 0
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

// sync godoc
//
//	@Summary		Refresh cached online state from the camera platform
//	@Description	Pulls current status for every linked device. The background poller does this on a timer; this endpoint is for the install flow, where waiting a minute to see whether the unit came online is the whole question.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	map[string]int
//	@Router			/v1/dashcams/sync [post]
func (h *Handler) sync(w http.ResponseWriter, r *http.Request) {
	n, err := Sync(r.Context(), h.pool, h.vss, auth.TenantID(r.Context()))
	if err != nil {
		if h.unavailable(w, err) {
			return
		}
		log.Printf("dashcam: sync: %v", err)
		httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int{"updated": n})
}

// live godoc
//
//	@Summary		Embeddable live-view URL for a dashcam
//	@Description	Returns a short-lived VSS player URL. The browser loads it in an iframe and streams from VSS directly.
//	@Tags			dashcams
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path	int	true	"dashcam id"
//	@Success		200	{object}	map[string]string
//	@Failure		503	{object}	map[string]string
//	@Router			/v1/dashcams/{id}/live [get]
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	dev, chans, ok := h.resolve(w, r)
	if !ok {
		return
	}
	u, err := h.vss.LiveURL(r.Context(), dev, chans, r.URL.Query().Get("stream") == "main")
	if err != nil {
		if h.unavailable(w, err) {
			return
		}
		log.Printf("dashcam: live url: %v", err)
		httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"url": u})
}

// playback godoc
//
//	@Summary	Embeddable playback URL for a time range
//	@Tags		dashcams
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id		path	int		true	"dashcam id"
//	@Param		from	query	string	true	"RFC3339"
//	@Param		to		query	string	true	"RFC3339"
//	@Success	200		{object}	map[string]string
//	@Router		/v1/dashcams/{id}/playback [get]
func (h *Handler) playback(w http.ResponseWriter, r *http.Request) {
	dev, chans, ok := h.resolve(w, r)
	if !ok {
		return
	}
	from, err1 := time.Parse(time.RFC3339, r.URL.Query().Get("from"))
	to, err2 := time.Parse(time.RFC3339, r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || !to.After(from) {
		httpx.Error(w, http.StatusBadRequest, "from and to must be RFC3339, with to after from")
		return
	}
	u, err := h.vss.PlaybackURL(r.Context(), dev, chans, from, to)
	if err != nil {
		if h.unavailable(w, err) {
			return
		}
		httpx.Error(w, http.StatusBadGateway, "camera platform unreachable")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"url": u})
}

// resolve loads the device id and channel list for a dashcam, scoped to the
// caller's tenant — so one customer cannot mint a video URL for another's
// camera by guessing an id.
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) (string, []int, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return "", nil, false
	}
	var deviceID string
	var channels int
	if err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(),
			`SELECT vss_device_id, channels FROM dashcams WHERE id=$1`, id).Scan(&deviceID, &channels)
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "dashcam not found")
		return "", nil, false
	}
	// ?ch=1_2 selects channels; otherwise every channel the unit reports, so a
	// two-camera install shows road and cabin without configuration.
	chans := parseChannels(r.URL.Query().Get("ch"), channels)
	return deviceID, chans, true
}

func parseChannels(q string, count int) []int {
	if q != "" {
		out := []int{}
		for _, p := range strings.Split(q, "_") {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				out = append(out, n)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if count <= 0 {
		return []int{1}
	}
	out := make([]int, 0, count)
	for i := 1; i <= count; i++ {
		out = append(out, i)
	}
	return out
}
