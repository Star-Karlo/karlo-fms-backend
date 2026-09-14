package platformadmin

// Backfill pushes rows that only ever reached fms_app up into master data.
//
// WHAT IT IS FOR. Between the cutover and write-through landing, FMS's vehicle
// screen wrote to fms_app and stopped there. Those vehicles are invisible to
// TMS — the user created three trucks for DEMO ACCOUNT and Control Tower showed
// none — and they carry no master_data_id, which also puts them outside
// vehiclesync's reach: its sweeps deliberately skip unpaired rows, so nothing
// was ever going to notice or fix them.
//
// Write-through stops the set growing. This clears what had already
// accumulated, once.
//
// WHY IT IS AN OPERATOR ACTION AND NOT A BACKGROUND JOB. Master data's HTTP API
// authorises a USER, and correctly refuses a caller who lacks vehicles.edit.
// A background loop has no user to forward, so making this automatic would have
// meant giving FMS a service-token path that writes as nobody — precisely the
// bypass the write-through design rejected. Someone triggers this instead, their
// bearer is forwarded, master data applies its own rules, and the audit trail
// names a person.
//
// SAFE TO RUN TWICE. Every step is conditional on master_data_id IS NULL, so a
// second run finds nothing to do rather than creating duplicates upstream.

import (
	"errors"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/store"
)

// BackfillResult reports what one run did, per company.
type BackfillResult struct {
	TenantID int64    `json:"tenant_id"`
	Examined int      `json:"examined"`
	Pushed   int      `json:"pushed"`
	Failed   int      `json:"failed"`
	DryRun   bool     `json:"dry_run"`
	Plates   []string `json:"plates"`
	Errors   []string `json:"errors,omitempty"`
}

// backfillVehicles pushes this account's unpaired vehicles into master data.
//
//	@Summary		Push FMS-only vehicles up into master data
//	@Description	Vehicles created in FMS before write-through existed have no master_data_id and are invisible to TMS. This creates them upstream and records the pairing. Idempotent. Pass ?dry_run=true to list what would be pushed without writing.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Param			account_id	query		int		false	"the account to backfill (defaults to the caller's)"
//	@Param			dry_run		query		bool	false	"report without writing"
//	@Success		200			{object}	platformadmin.BackfillResult
//	@Router			/v1/platform/backfill-vehicles [post]
func (h *Handler) backfillVehicles(w http.ResponseWriter, r *http.Request) {
	if h.md == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"write-through is not configured on this deployment (MD_SERVICE_URL unset), so there is nowhere to push to")
		return
	}
	tid := auth.EffectiveTenant(r)
	dry := r.URL.Query().Get("dry_run") == "true"
	// The operator is staff acting for a customer via ?account_id, so the
	// acting-for MUST be translated or every vehicle is pushed under Karlo
	// Platform instead of the customer. Refused outright if it cannot be.
	caller, err := mdwrite.CallerFor(r, auth.TenantID(r.Context()), tid, h.dir)
	if err != nil {
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}

	var rows []store.Vehicle
	if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		var err error
		rows, err = store.New(tx).ListUnpairedVehicles(r.Context(), tid)
		return err
	}); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := BackfillResult{TenantID: tid, Examined: len(rows), DryRun: dry, Plates: []string{}}
	for _, v := range rows {
		out.Plates = append(out.Plates, v.LicensePlate)
		if dry {
			continue
		}

		// One vehicle at a time, each in its own upstream call and its own
		// local transaction. A batch would be faster and much worse: one
		// rejected plate would roll back the pairing of every vehicle already
		// created upstream, and the next run would create them all a second
		// time. Per-row means a failure costs exactly that row.
		mv := mdwrite.Vehicle{
			LicensePlate: v.LicensePlate,
			UnitType:     "rigid", // master data requires one; FMS has no equivalent field
			Status:       v.Status,
			Attributes:   map[string]any{"fmsVehicleId": v.ID},
		}
		if v.HullNo != nil {
			mv.ChassisNumber = *v.HullNo
		}
		if v.UnitYear != nil {
			mv.Year = *v.UnitYear
		}

		mdID, err := h.md.CreateVehicle(r.Context(), caller, mv)
		if err != nil {
			out.Failed++
			out.Errors = append(out.Errors, v.LicensePlate+": "+err.Error())
			var me *mdwrite.Error
			// A permission refusal will apply to every remaining row too, so
			// stop rather than generate one identical error per vehicle.
			if errors.As(err, &me) && (me.Status == http.StatusForbidden || me.Status == http.StatusUnauthorized) {
				out.Errors = append(out.Errors, "stopping: master data refuses this user, the rest would fail the same way")
				break
			}
			continue
		}

		// Record the pairing immediately. If this fails the vehicle exists
		// upstream but unpaired here, and the next run would create a DUPLICATE
		// — so it is reported loudly rather than counted as a success.
		if err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
			_, err := store.New(tx).PairVehicle(r.Context(), store.PairVehicleParams{ID: v.ID, MasterDataID: &mdID})
			return err
		}); err != nil {
			out.Failed++
			out.Errors = append(out.Errors,
				v.LicensePlate+": created upstream as "+mdID+" but the local pairing failed — DO NOT re-run until this row is paired by hand, or it will be created twice: "+err.Error())
			log.Printf("backfill: vehicle %d created as %s but pairing failed: %v", v.ID, mdID, err)
			continue
		}
		out.Pushed++
	}

	log.Printf("backfill: tenant %d — %d examined, %d pushed, %d failed (dry_run=%v)",
		tid, out.Examined, out.Pushed, out.Failed, dry)
	httpx.JSON(w, http.StatusOK, out)
}

// backfillRoutes mounts the staff-only backfill endpoints.
func (h *Handler) backfillRoutes(r chi.Router) {
	r.Post("/v1/platform/backfill-vehicles", h.backfillVehicles)
}
