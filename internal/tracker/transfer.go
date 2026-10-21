package tracker

import (
	"log"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Unlinking a device from its truck, and handing it to another company.
//
// Both were missing in opposite directions. A device could be bound to a
// vehicle and swapped for another, but never simply taken off — the only
// way to free one was to DELETE it, which threw away the device record and
// its fit history to achieve "this truck has no tracker today". And a
// device sold or lent to another customer had no path at all: somebody
// deleted it here and registered it there, which breaks the history on both
// sides and leaves master data holding a device under a company that no
// longer has it.

// UnassignResponse reports what the unlink left behind.
type UnassignResponse struct {
	DTO
	// PreviousVehicleID is the truck it came off, so a caller can say what
	// changed rather than just that something did.
	PreviousVehicleID *int64 `json:"previous_vehicle_id"`
}

// unassign godoc
//
//	@Summary		Take a tracker off its vehicle (keep the device)
//	@Description	Clears the link from BOTH sides — the vehicle's tracker and the tracker's vehicle — and closes the open period in the fit history, so the device is free to fit elsewhere and the truck shows as untracked. The device itself is kept, with its history; deleting it is a different action. Mirrored to master data as an unfit. Does nothing (and answers 200) if the device is already unassigned.
//	@Tags			trackers
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"tracker id"
//	@Success		200	{object}	tracker.UnassignResponse
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/trackers/{id}/unassign [post]
func (h *Handler) unassign(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	var out UnassignResponse
	var mdID *string
	err := db.Tx(r.Context(), h.pool, auth.EffectiveTenant(r), func(tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetTracker(r.Context(), id)
		if err != nil {
			return err
		}
		out.PreviousVehicleID = before.VehicleID
		mdID = before.MasterDataID
		// The vehicle side first: trackers.vehicle_id is what the
		// assignment-history trigger watches, so clearing it last means the
		// period closes with both sides already consistent.
		if err := q.ClearVehicleTracker(r.Context(), &id); err != nil {
			return err
		}
		t, err := q.UnassignTracker(r.Context(), id)
		if err != nil {
			return err
		}
		out.DTO = fromRow(t)
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "tracker not found")
		return
	}
	if h.md != nil && mdID != nil && out.PreviousVehicleID != nil {
		if err := h.md.UnfitTracker(r.Context(), caller, *mdID); err != nil {
			log.Printf("tracker %d: unassigned locally, master data %s rejected the unfit: %v", id, *mdID, err)
		}
	}
	httpx.JSON(w, http.StatusOK, out)
}

// TransferRequest names the account the device is moving to.
type TransferRequest struct {
	TenantID int64 `json:"tenant_id" example:"5"`
}

// TransferResponse says what happened, in the order it happened.
type TransferResponse struct {
	TrackerID int64  `json:"tracker_id"`
	IMEI      string `json:"imei"`
	FromName  string `json:"from"`
	ToName    string `json:"to"`
	// Unassigned is the vehicle it was taken off on the way out, if any.
	Unassigned *int64 `json:"unassigned_from_vehicle,omitempty"`
	// Note explains what the caller should expect to see next.
	Note string `json:"note"`
}

// transfer godoc
//
//	@Summary		Hand a tracker to another account (platform admin)
//	@Description	Moves ownership of a device. Master data owns the device register, so the authoritative write happens there; this service then takes the device off whatever truck it was on, marks its own row inactive, and lets the projection bring it in under the new account — which keeps the OLD account's fit history readable, so telemetry recorded while the device was theirs still resolves to their truck. The device appears for the new account within a minute (sooner on the master-data notice). Refuses if the device is not paired with master data, because an unpaired device cannot be moved anywhere that would stick.
//	@Tags			trackers
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int							true	"tracker id"
//	@Param			body	body		tracker.TransferRequest		true	"destination account"
//	@Success		200		{object}	tracker.TransferResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		409		{object}	map[string]string	"not paired with master data, or the destination has no company"
//	@Failure		502		{object}	map[string]string	"master data refused the transfer"
//	@Router			/v1/trackers/{id}/transfer [post]
func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	id, ok := id64(r)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req TransferRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.TenantID == 0 {
		httpx.Error(w, http.StatusBadRequest, "tenant_id is required")
		return
	}
	from := auth.EffectiveTenant(r)
	if req.TenantID == from {
		httpx.Error(w, http.StatusBadRequest, "the device already belongs to that account")
		return
	}
	if h.md == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	toCompany, ok := h.dir.CompanyForTenant(r.Context(), req.TenantID)
	if !ok || toCompany == "" {
		httpx.Error(w, http.StatusConflict, "the destination account is not paired with a company in master data")
		return
	}

	var imei string
	var mdID *string
	var prevVehicle *int64
	err := db.Tx(r.Context(), h.pool, from, func(tx pgx.Tx) error {
		t, err := store.New(tx).GetTracker(r.Context(), id)
		if err != nil {
			return err
		}
		imei, mdID, prevVehicle = t.Imei, t.MasterDataID, t.VehicleID
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "tracker not found in this account")
		return
	}
	if mdID == nil {
		httpx.Error(w, http.StatusConflict, "this device is not paired with master data yet, so ownership cannot be moved; wait for the projection or register it upstream first")
		return
	}

	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	// Master data first. If it refuses, nothing has changed anywhere — the
	// opposite order would leave a device this account cannot see and the
	// other cannot claim.
	if err := h.md.TransferTracker(r.Context(), caller, *mdID, toCompany); err != nil {
		httpx.Error(w, http.StatusBadGateway, "master data refused the transfer: "+err.Error())
		return
	}

	// Locally: off the truck, and parked inactive rather than deleted.
	// Deleting would cascade the fit history away, and that history is how
	// last month's positions still resolve to the truck that carried them.
	if err := db.Tx(r.Context(), h.pool, from, func(tx pgx.Tx) error {
		q := store.New(tx)
		if err := q.ClearVehicleTracker(r.Context(), &id); err != nil {
			return err
		}
		return q.DeactivateTracker(r.Context(), id)
	}); err != nil {
		// Upstream already moved, so this is a reconciliation problem, not a
		// failure of the transfer: say so rather than implying nothing happened.
		log.Printf("tracker %d: transferred upstream to %s but the local row could not be retired: %v", id, toCompany, err)
		httpx.Error(w, http.StatusInternalServerError, "ownership moved in master data, but this account's copy could not be retired — the projection will correct it")
		return
	}
	for _, wk := range h.wakers {
		wk.Wake() // bring it in under the new account now rather than at the next tick
	}

	httpx.JSON(w, http.StatusOK, TransferResponse{
		TrackerID: id, IMEI: imei,
		FromName: h.tenantName(r, from), ToName: h.tenantName(r, req.TenantID),
		Unassigned: prevVehicle,
		Note:       "ownership moved in master data; this account keeps the device inactive with its history, and the new account receives it on the next projection pass",
	})
}

// tenantName is best-effort labelling for the response.
func (h *Handler) tenantName(r *http.Request, id int64) string {
	var name string
	_ = h.pool.QueryRow(r.Context(), `SELECT name FROM tenants WHERE id = $1`, id).Scan(&name)
	return name
}

// WithSyncWakers lets a transfer ask the device projection to run now.
func (h *Handler) WithSyncWakers(w ...interface{ Wake() }) *Handler { h.wakers = w; return h }
