// Package trackinglink issues shareable, time-limited public tracking links so
// an external party can view selected vehicles' live location without an account.
package trackinglink

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/liveview"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON shape of a tracking link.
type DTO struct {
	ID         int64           `json:"id"`
	Token      string          `json:"token"`
	Name       string          `json:"name"`
	Note       *string         `json:"note"`
	Status     string          `json:"status"`
	ExpiresAt  *time.Time      `json:"expires_at"`
	ShowFields json.RawMessage `json:"show_fields"`
	VehicleIDs json.RawMessage `json:"vehicle_ids"`
	CreatedAt  *time.Time      `json:"created_at"`
}

// WriteRequest is the create/update body.
type WriteRequest struct {
	Name        string          `json:"name"`
	Note        *string         `json:"note"`
	Status      *string         `json:"status"`
	DurationDays int            `json:"duration_days"`
	DurationHours int           `json:"duration_hours"` // takes precedence over days when set
	ExpiresAt   *string         `json:"expires_at"`
	ShowFields  json.RawMessage `json:"show_fields"`
	VehicleIDs  []int64         `json:"vehicle_ids"`
}

func fromRow(l store.TrackingLink) DTO {
	return DTO{
		ID: l.ID, Token: l.Token, Name: l.Name, Note: l.Note, Status: l.Status,
		ExpiresAt: conv.Time(l.ExpiresAt), ShowFields: json.RawMessage(l.ShowFields),
		VehicleIDs: json.RawMessage(l.VehicleIds), CreatedAt: conv.Time(l.CreatedAt),
	}
}

func newToken() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Handler exposes tracking-link endpoints (authenticated CRUD + a public view).
type Handler struct {
	pool *db.Pool
	live *liveview.Service
}

// NewHandler builds the handler.
func NewHandler(pool *db.Pool, live *liveview.Service) *Handler {
	return &Handler{pool: pool, live: live}
}

// Routes mounts the authenticated CRUD endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/tracking-links", h.list)
	r.Get("/v1/tracking-links/{id}", h.get)
	r.Post("/v1/tracking-links", h.create)
	r.Put("/v1/tracking-links/{id}", h.update)
	r.Delete("/v1/tracking-links/{id}", h.del)
}

// PublicRoutes mounts the unauthenticated share endpoint.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Get("/public/track/{token}", h.public)
}

func id64(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id, err == nil
}

// list godoc
//
//	@Summary	List tracking links
//	@Tags		tracking-links
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[trackinglink.DTO]
//	@Router		/v1/tracking-links [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	tid := auth.TenantID(r.Context())
	var items []DTO
	var total int64
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		rows, err := st.ListTrackingLinks(r.Context(), store.ListTrackingLinksParams{Q: conv.StrPtr(p.Q), Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, l := range rows {
			items = append(items, fromRow(l))
		}
		total, err = st.CountTrackingLinks(r.Context(), conv.StrPtr(p.Q))
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: total})
}

// get godoc
//
//	@Summary	Get a tracking link
//	@Tags		tracking-links
//	@Security	BearerAuth
//	@Produce	json
//	@Param		id	path		int	true	"id"
//	@Success	200	{object}	trackinglink.DTO
//	@Router		/v1/tracking-links/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		l, err := store.New(tx).GetTrackingLink(r.Context(), id)
		if err != nil {
			return err
		}
		dto = fromRow(l)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

func expiryFrom(req WriteRequest) (pgtype.Timestamptz, bool) {
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, *req.ExpiresAt); err == nil {
			return pgtype.Timestamptz{Time: t, Valid: true}, true
		}
		return pgtype.Timestamptz{}, false
	}
	if req.DurationHours > 0 {
		return pgtype.Timestamptz{Time: time.Now().Add(time.Duration(req.DurationHours) * time.Hour), Valid: true}, true
	}
	days := req.DurationDays
	if days <= 0 {
		days = 7
	}
	return pgtype.Timestamptz{Time: time.Now().Add(time.Duration(days) * 24 * time.Hour), Valid: true}, true
}

func vehicleIDsJSON(ids []int64) []byte {
	if ids == nil {
		ids = []int64{}
	}
	b, _ := json.Marshal(ids)
	return b
}

// create godoc
//
//	@Summary	Create a tracking link
//	@Tags		tracking-links
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		trackinglink.WriteRequest	true	"link"
//	@Success	201		{object}	trackinglink.DTO
//	@Router		/v1/tracking-links [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req WriteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	exp, ok := expiryFrom(req)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid expires_at")
		return
	}
	tid := auth.TenantID(r.Context())
	var dto DTO
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		l, err := store.New(tx).CreateTrackingLink(r.Context(), store.CreateTrackingLinkParams{
			TenantID: tid, Token: newToken(), Name: req.Name, Note: req.Note,
			Status: ifaceStr(req.Status), ExpiresAt: exp,
			ShowFields: jsonBytes(req.ShowFields), VehicleIds: vehicleIDsJSON(req.VehicleIDs),
		})
		if err != nil {
			return err
		}
		dto = fromRow(l)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// update godoc
//
//	@Summary	Update a tracking link
//	@Tags		tracking-links
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		id		path		int							true	"id"
//	@Param		body	body		trackinglink.WriteRequest	true	"link"
//	@Success	200		{object}	trackinglink.DTO
//	@Router		/v1/tracking-links/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req WriteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	var expPtr pgtype.Timestamptz
	if req.ExpiresAt != nil || req.DurationDays > 0 {
		e, valid := expiryFrom(req)
		if !valid {
			httpx.Error(w, http.StatusBadRequest, "invalid expires_at")
			return
		}
		expPtr = e
	}
	var vids []byte
	if req.VehicleIDs != nil {
		vids = vehicleIDsJSON(req.VehicleIDs)
	}
	var dto DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		l, err := store.New(tx).UpdateTrackingLink(r.Context(), store.UpdateTrackingLinkParams{
			ID: id, Name: conv.StrPtr(req.Name), Note: req.Note, Status: req.Status,
			ExpiresAt: expPtr, ShowFields: jsonBytes(req.ShowFields), VehicleIds: vids,
		})
		if err != nil {
			return err
		}
		dto = fromRow(l)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, dto)
}

// del godoc
//
//	@Summary	Delete a tracking link
//	@Tags		tracking-links
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/tracking-links/{id} [delete]
func (h *Handler) del(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).DeleteTrackingLink(r.Context(), id)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublicVehicle is a privacy-filtered vehicle position for the share page.
type PublicVehicle struct {
	Plate    string   `json:"license_plate"`
	Lat      *float64 `json:"lat"`
	Lon      *float64 `json:"lon"`
	Bearing  *float64 `json:"bearing"`     // heading for the truck icon
	State    string   `json:"drive_state"` // moving|idle|parking|offline (icon color)
	Speed    *float64 `json:"speed,omitempty"`
	Driver   *string  `json:"driver,omitempty"`
	LastSeen *string  `json:"last_seen,omitempty"`
	Machine  *bool    `json:"machine_on,omitempty"`
	Online   bool     `json:"online"`
}

// PublicResponse is the share-page payload.
type PublicResponse struct {
	Name      string          `json:"name"`
	ExpiresAt *time.Time      `json:"expires_at"`
	Vehicles  []PublicVehicle `json:"vehicles"`
}

// public godoc
//
//	@Summary	Public live view for a shared tracking link (no auth)
//	@Tags		tracking-links
//	@Produce	json
//	@Param		token	path	string	true	"link token"
//	@Success	200	{object}	trackinglink.PublicResponse
//	@Failure	404	{object}	map[string]string
//	@Failure	410	{object}	map[string]string
//	@Router		/public/track/{token} [get]
func (h *Handler) public(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	// Resolve the link unscoped (permissive RLS allows token lookup).
	var link store.TrackingLink
	if err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		l, err := store.New(tx).GetTrackingLinkByToken(r.Context(), token)
		link = l
		return err
	}); err != nil {
		httpx.Error(w, http.StatusNotFound, "link not found")
		return
	}
	if link.Status != "active" || (link.ExpiresAt.Valid && link.ExpiresAt.Time.Before(time.Now())) {
		httpx.Error(w, http.StatusGone, "link expired or inactive")
		return
	}

	var show map[string]bool
	_ = json.Unmarshal(link.ShowFields, &show)
	var vids []int64
	_ = json.Unmarshal(link.VehicleIds, &vids)

	resp := PublicResponse{Name: link.Name, ExpiresAt: conv.Time(link.ExpiresAt)}
	for _, vid := range vids {
		v, err := h.live.One(r.Context(), link.TenantID, vid)
		if err != nil {
			continue
		}
		pv := PublicVehicle{Plate: v.LicensePlate, Online: v.Online, State: v.DriveState}
		if v.Position != nil {
			pv.Lat, pv.Lon = v.Position.Lat, v.Position.Lon
			pv.Bearing = v.Position.Bearing
			if show["speed"] {
				pv.Speed = v.Position.Speed
			}
			if show["machine"] {
				pv.Machine = v.Position.Ignition
			}
			if show["last_seen"] {
				t := v.Position.Time.Format(time.RFC3339)
				pv.LastSeen = &t
			}
		}
		if show["driver"] {
			pv.Driver = v.Driver.Name
		}
		resp.Vehicles = append(resp.Vehicles, pv)
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func ifaceStr(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

func jsonBytes(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	return []byte(raw)
}
