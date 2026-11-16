package liveview

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// IntegrationRoutes mounts the external read API used by other platforms (e.g.
// the TMS) via a per-tenant API token. Auth is applied by the caller's route
// group (the api-token authenticator in main.go), which sets the tenant in
// context — so these handlers read auth.TenantID like any other, and RLS scopes
// every query to the token's account. Keyed by IMEI, not FMS's internal id, so
// an integrator uses the identifier it already shares with the tracking service.
func (h *Handler) IntegrationRoutes(r chi.Router) {
	r.Get("/v1/integration/vehicles/{imei}/analytics", h.integrationAnalytics)
	// Addressed by nomor polisi or hull number rather than by IMEI or by this
	// system's id — the numbers an outside system actually has. Mounted here,
	// not in the JWT group, because the callers are integrations holding an API key;
	// the group accepts a signed-in user too, so the API docs page still
	// works (see apitoken.AuthenticatorOr).
	r.Get("/v1/vehicles/by-plate/{plate}/history", h.historyByPlate)
	r.Get("/v1/vehicles/by-plate/{plate}/live", h.liveByPlate)
	r.Get("/v1/vehicles/by-plate/{plate}/behavior", h.behaviorByPlate)
	r.Get("/v1/vehicles/by-hull/{hull_number}/behavior", h.behaviorByHull)
	r.Get("/v1/vehicles/by-plate/{plate}/fuel-trip", h.fuelTripByPlate)
	r.Get("/v1/vehicles/by-plate/{plate}/driving-events", h.drivingEventsByPlate)
	r.Post("/v1/vehicles/history-summary", h.summary)
}

// integrationAnalytics godoc
//
//	@Summary		External vehicle analytics by IMEI (history + calculations)
//	@Description	Same consolidated analytics as /v1/vehicles/{id}/analytics, but keyed by device IMEI and authenticated with a per-tenant API token (header `X-Api-Key: fms_...` or `Authorization: Bearer fms_...`). The IMEI is resolved within the token's account only.
//	@Tags			integration
//	@Produce		json
//	@Param			imei			path	string	true	"device IMEI"
//	@Param			from			query	string	true	"RFC3339"
//	@Param			to				query	string	true	"RFC3339"
//	@Param			limit			query	number	false	"overspeed limit km/h (default 60)"
//	@Param			idle_threshold	query	number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Success		200	{object}	liveview.VehicleAnalytics
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/integration/vehicles/{imei}/analytics [get]
func (h *Handler) integrationAnalytics(w http.ResponseWriter, r *http.Request) {
	imei := chi.URLParam(r, "imei")
	if imei == "" {
		httpx.Error(w, http.StatusBadRequest, "imei is required")
		return
	}
	q := r.URL.Query()
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		httpx.Error(w, http.StatusBadRequest, "from/to must be RFC3339")
		return
	}
	limit := 60.0
	if v := q.Get("limit"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			limit = f
		}
	}
	idleThreshold := 120.0
	if v := q.Get("idle_threshold"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			idleThreshold = f
		}
	}

	ctx := r.Context()
	tenantID := auth.TenantID(ctx)
	vid, ok, err := h.svc.VehicleIDByIMEI(ctx, tenantID, imei)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no vehicle with that IMEI in this account")
		return
	}
	httpx.JSON(w, http.StatusOK, h.svc.Analytics(ctx, tenantID, vid, from, to, limit, idleThreshold))
}
