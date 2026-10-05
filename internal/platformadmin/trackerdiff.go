package platformadmin

import (
	"context"
	"net/http"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Does FMS agree with master data about which truck each device is on?
//
// Master data owns the device register; FMS keeps its own trackers table and
// its own fit history, written by a trigger long before that ownership was
// settled. Both have been maintained independently, so before anything is
// switched over — the writes, or the history resolution — somebody has to
// see how far apart they actually are. A correction that silently moves a
// live mapping is worse than the mis-attribution it fixes: positions would
// start landing on a different truck with nobody told.
//
// This compares the CURRENT fitting only, which is what master data's gRPC
// exposes today. The history comparison needs the periods endpoint their
// side is building; this answers the question that decides whether a
// switchover is safe at all.

// TrackerDiffRow is one device the two systems describe differently, or
// agree about — `verdict` says which.
type TrackerDiffRow struct {
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	DeviceID   string `json:"device_id"` // IMEI
	// Verdict:
	//   agree          — both say the same vehicle (or both say unfitted)
	//   differ         — fitted to DIFFERENT vehicles. The dangerous one.
	//   fms_only       — FMS has it on a truck, master data has it in stock
	//   masterdata_only— master data has it on a truck, FMS has it unfitted
	//   unpaired       — FMS has no master_data_id for the device yet
	//   unknown_vehicle— master data names a vehicle FMS cannot resolve
	Verdict string `json:"verdict"`
	// What each side says, as a plate where FMS can resolve one.
	FMSVehicle        string `json:"fms_vehicle,omitempty"`
	MasterDataVehicle string `json:"masterdata_vehicle,omitempty"`
	IsDemo            bool   `json:"is_demo,omitempty"`
}

// TrackerDiff is the report.
type TrackerDiff struct {
	Counts map[string]int   `json:"counts"`
	Rows   []TrackerDiffRow `json:"rows"`
	// Checked is how many devices were compared.
	Checked int `json:"checked"`
}

// trackerDiff godoc
//
//	@Summary		Compare FMS device fittings with master data (platform admin)
//	@Description	For every account, the device register as FMS holds it against the register in master data, device by device. Verdicts: agree, differ (fitted to different vehicles — the one that matters), fms_only, masterdata_only, unpaired (no master_data_id here yet), unknown_vehicle (master data names a vehicle this account cannot resolve). Rows that agree are summarised in the counts and omitted from the list. Read-only: nothing is written anywhere. Run this before switching device writes or history resolution over, so a correction cannot silently move a live mapping.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	platformadmin.TrackerDiff
//	@Failure		503	{object}	map[string]string	"master data not configured"
//	@Router			/v1/platform/trackers/diff [get]
func (h *Handler) trackerDiff(w http.ResponseWriter, r *http.Request) {
	if h.mdRead == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	out := TrackerDiff{Counts: map[string]int{}}
	for _, t := range tenants {
		company, _ := h.dir.CompanyForTenant(r.Context(), t.id)
		if company == "" {
			continue
		}
		rows, n, err := h.diffTenant(r.Context(), t, company)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "diff failed for "+t.name+": "+err.Error())
			return
		}
		out.Checked += n
		for _, row := range rows {
			out.Counts[row.Verdict]++
			if row.Verdict != "agree" {
				out.Rows = append(out.Rows, row)
			}
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].Verdict != out.Rows[j].Verdict {
			return out.Rows[i].Verdict < out.Rows[j].Verdict
		}
		return out.Rows[i].DeviceID < out.Rows[j].DeviceID
	})
	httpx.JSON(w, http.StatusOK, out)
}

// fmsDevice is what this service believes about one device.
type fmsDevice struct {
	masterDataID string
	vehiclePlate string
	vehicleMDID  string
	isDemo       bool
}

func (h *Handler) diffTenant(ctx context.Context, t tenantRef, company string) ([]TrackerDiffRow, int, error) {
	// What master data says, by device id.
	upstream := map[string]string{} // device id -> its vehicle's master-data id
	known := map[string]bool{}      // device ids master data holds for this company
	devices, err := h.mdRead.ListTrackers(ctx, company, "gps")
	if err != nil {
		return nil, 0, err
	}
	for _, d := range devices {
		if d.Deleted {
			continue
		}
		known[d.DeviceID] = true
		upstream[d.DeviceID] = d.CurrentVehicleID
	}

	// What FMS says, by device id.
	local := map[string]fmsDevice{}
	plateByMDID := map[string]string{}
	err = db.Tx(ctx, h.pool, t.id, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tr.imei, coalesce(tr.master_data_id,''), tr.is_demo,
			       coalesce(v.license_plate,''), coalesce(v.master_data_id,'')
			  FROM trackers tr
			  LEFT JOIN vehicles v ON v.id = tr.vehicle_id
			 WHERE tr.status <> 'inactive'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var imei string
			var d fmsDevice
			if err := rows.Scan(&imei, &d.masterDataID, &d.isDemo, &d.vehiclePlate, &d.vehicleMDID); err != nil {
				return err
			}
			local[imei] = d
		}
		if err := rows.Err(); err != nil {
			return err
		}
		// Plates for the vehicles master data names, so a difference reads
		// as "B 1234 XY vs B 9999 ZZ" rather than two opaque ids.
		vr, err := tx.Query(ctx, `SELECT master_data_id, license_plate FROM vehicles WHERE master_data_id IS NOT NULL`)
		if err != nil {
			return err
		}
		defer vr.Close()
		for vr.Next() {
			var id, plate string
			if err := vr.Scan(&id, &plate); err != nil {
				return err
			}
			plateByMDID[id] = plate
		}
		return vr.Err()
	})
	if err != nil {
		return nil, 0, err
	}

	seen := map[string]bool{}
	var out []TrackerDiffRow
	add := func(deviceID string, d fmsDevice, mdVehicle string) {
		seen[deviceID] = true
		row := TrackerDiffRow{
			TenantID: t.id, TenantName: t.name, DeviceID: deviceID,
			FMSVehicle: d.vehiclePlate, IsDemo: d.isDemo,
		}
		if mdVehicle != "" {
			if p, ok := plateByMDID[mdVehicle]; ok {
				row.MasterDataVehicle = p
			} else {
				row.MasterDataVehicle = mdVehicle // an id this account cannot resolve
			}
		}
		switch {
		case d.isDemo:
			// A demo copy is deliberately not master data's: excluded from
			// the comparison rather than reported as a disagreement.
			row.Verdict = "agree"
		case d.masterDataID == "":
			row.Verdict = "unpaired"
		case d.vehicleMDID == "" && mdVehicle == "":
			row.Verdict = "agree" // both say in stock
		case d.vehicleMDID == mdVehicle:
			row.Verdict = "agree"
		case mdVehicle == "":
			row.Verdict = "fms_only"
		case d.vehicleMDID == "":
			row.Verdict = "masterdata_only"
		case row.MasterDataVehicle == mdVehicle:
			row.Verdict = "unknown_vehicle" // named a vehicle we cannot resolve
		default:
			row.Verdict = "differ"
		}
		out = append(out, row)
	}

	for imei, d := range local {
		add(imei, d, upstream[imei])
	}
	// Devices master data holds that FMS has never seen at all.
	for id := range known {
		if !seen[id] {
			out = append(out, TrackerDiffRow{
				TenantID: t.id, TenantName: t.name, DeviceID: id,
				Verdict: "masterdata_only", MasterDataVehicle: plateByMDID[upstream[id]],
			})
		}
	}
	return out, len(out), nil
}
