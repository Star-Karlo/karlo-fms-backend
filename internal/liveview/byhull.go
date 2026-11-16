package liveview

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Asking for behaviour by the hull number (nomor lambung) painted on the cab,
// for integrations that know a truck by that rather than by its plate.
//
// AMBIGUITY IS REFUSED, unlike plates. Plates are unique per account in this
// table; hull numbers are unique only in master data, and this table's copy
// can drift (a value cleared upstream is never cleared here). Answering for
// one of two trucks would return the wrong truck's distance with a 200, so a
// tie is a 409 naming the plates, and the caller can fix the data or ask by
// plate instead.

// pickHull chooses the vehicle a hull number names. A vehicle still in the
// fleet (active or in repair) wins over archived ones, because a number is
// reassigned when a truck leaves. Returns the plates when more than one
// vehicle is still tied, and 0 with no plates when nothing matched.
func pickHull(rows []store.ListVehiclesByHullRow) (int64, []string) {
	var fleet, archived []store.ListVehiclesByHullRow
	for _, v := range rows {
		if v.Status == "archive" {
			archived = append(archived, v)
		} else {
			fleet = append(fleet, v)
		}
	}
	tier := fleet
	if len(tier) == 0 {
		tier = archived
	}
	switch len(tier) {
	case 0:
		return 0, nil
	case 1:
		return tier[0].ID, nil
	}
	plates := make([]string, len(tier))
	for i, v := range tier {
		plates[i] = v.LicensePlate
	}
	return 0, plates
}

// VehicleIDByHull resolves a hull number to this tenant's vehicle id. ok is
// false when nothing matched; ambiguous lists the plates when several did.
func (s *Service) VehicleIDByHull(ctx context.Context, tenantID int64, hull string) (id int64, ambiguous []string, ok bool, err error) {
	norm := NormalisePlate(hull)
	if norm == "" {
		return 0, nil, false, nil
	}
	var rows []store.ListVehiclesByHullRow
	err = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		rows, err = store.New(tx).ListVehiclesByHull(ctx, norm)
		return err
	})
	if err != nil {
		return 0, nil, false, err
	}
	id, ambiguous = pickHull(rows)
	return id, ambiguous, id != 0 || len(ambiguous) > 0, nil
}

// resolveHull answers 404, 409 or 500 itself when the hull number does not
// name exactly one vehicle.
func (h *Handler) resolveHull(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := chi.URLParam(r, "hull_number")
	// chi routes on the escaped path, so a "/" sent as %2F arrives still escaped.
	hull, err := url.PathUnescape(raw)
	if err != nil {
		hull = raw
	}
	id, ambiguous, ok, err := h.svc.VehicleIDByHull(r.Context(), auth.EffectiveTenant(r), hull)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	if !ok {
		httpx.Error(w, http.StatusNotFound, "no vehicle with hull number "+hull+" in this account")
		return 0, false
	}
	if len(ambiguous) > 0 {
		httpx.Error(w, http.StatusConflict, "hull number "+hull+" matches "+strconv.Itoa(len(ambiguous))+
			" vehicles in this account: "+strings.Join(ambiguous, ", "))
		return 0, false
	}
	return id, true
}

// behaviorByHull godoc
//
//	@Summary		Driving behaviour for a window, addressed by hull number
//	@Description	Identical to /v1/vehicles/{vehicle_id}/behavior, with the hull number (nomor lambung) in place of the id. Spacing, dashes and case are ignored. A hull number shared by more than one vehicle in the account is refused with 409 rather than answered for one of them.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			hull_number		path		string	true	"hull number (nomor lambung), e.g. S3487"
//	@Param			from			query		string	true	"RFC3339"
//	@Param			to				query		string	true	"RFC3339"
//	@Param			limit			query		number	false	"overspeed limit km/h (default 60)"
//	@Param			idle_threshold	query		number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Success		200	{object}	liveview.BehaviorResult
//	@Failure		400	{object}	map[string]string
//	@Failure		401	{object}	map[string]string
//	@Failure		404	{object}	map[string]string
//	@Failure		409	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Failure		502	{object}	map[string]string
//	@Router			/v1/vehicles/by-hull/{hull_number}/behavior [get]
func (h *Handler) behaviorByHull(w http.ResponseWriter, r *http.Request) {
	id, ok := h.resolveHull(w, r)
	if !ok {
		return
	}
	h.behaviorFor(w, r, id)
}
