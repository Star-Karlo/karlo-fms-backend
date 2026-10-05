package platformadmin

import (
	"context"
	"net/http"
	"sort"
	"strconv"
	"time"

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

// ---------------------------------------------------------------------------
// History comparison
// ---------------------------------------------------------------------------

// The current fitting answers "is a switchover safe today". This answers the
// other half: do the two FIT HISTORIES agree about the past, which is what a
// historical read resolves against.
//
// Both sides are thin for the same reason and it matters for reading the
// result: FMS has rows only since migration 0040 and only for fits recorded
// through it (a CSV import with a vehicle attached leaves none), and master
// data has rows only for fits made through its Fit/Unfit API. So "one side
// has no row" is overwhelmingly "nobody recorded it", not "the other side
// invented it" — those are counted separately from a real conflict, where
// both sides describe the same device at the same time on DIFFERENT trucks.

// HistoryPeriod is one side's view of a device on a vehicle.
type HistoryPeriod struct {
	IMEI    string `json:"imei"`
	Vehicle string `json:"vehicle"` // plate where resolvable, else the master-data id
	From    string `json:"from"`
	To      string `json:"to,omitempty"` // empty = still fitted
}

// HistoryConflict is the case that actually matters: at one moment, the two
// systems put the same device on different trucks.
type HistoryConflict struct {
	TenantID   int64         `json:"tenant_id"`
	TenantName string        `json:"tenant_name"`
	IMEI       string        `json:"imei"`
	Overlap    string        `json:"overlap"` // the period they disagree about
	FMS        HistoryPeriod `json:"fms"`
	MasterData HistoryPeriod `json:"masterdata"`
}

// HistoryDiff is the report.
type HistoryDiff struct {
	Window    string            `json:"window"`
	Counts    map[string]int    `json:"counts"`
	Conflicts []HistoryConflict `json:"conflicts"`
}

// trackerHistoryDiff godoc
//
//	@Summary		Compare the device fit HISTORIES with master data (platform admin)
//	@Description	Periods, not just the current fitting: for each account, every stretch during which a device sat on a vehicle, as FMS recorded it and as master data recorded it. Counts are agreed (both describe the same device on the same vehicle over an overlapping period), fms_only and masterdata_only (the other side has no row — usually nobody recorded it rather than a conflict, since both histories only hold fits made through their own API), and conflict, which is the one that matters: at one moment the two put the same device on DIFFERENT trucks. Only conflicts are listed. Read-only. Default window is the last 365 days; pass ?days= to change it.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Param			days	query		int	false	"how far back to compare (default 365)"
//	@Success		200		{object}	platformadmin.HistoryDiff
//	@Failure		503		{object}	map[string]string	"master data not configured"
//	@Router			/v1/platform/trackers/history-diff [get]
func (h *Handler) trackerHistoryDiff(w http.ResponseWriter, r *http.Request) {
	if h.mdRead == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	days := 365
	if v := r.URL.Query().Get("days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 3650 {
			days = n
		}
	}
	to := time.Now()
	from := to.AddDate(0, 0, -days)

	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	out := HistoryDiff{
		Window: from.Format(time.RFC3339) + " .. " + to.Format(time.RFC3339),
		Counts: map[string]int{},
	}
	for _, t := range tenants {
		company, _ := h.dir.CompanyForTenant(r.Context(), t.id)
		if company == "" {
			continue
		}
		if err := h.historyDiffTenant(r.Context(), t, company, from, to, &out); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "history diff failed for "+t.name+": "+err.Error())
			return
		}
	}
	sort.Slice(out.Conflicts, func(i, j int) bool {
		if out.Conflicts[i].TenantID != out.Conflicts[j].TenantID {
			return out.Conflicts[i].TenantID < out.Conflicts[j].TenantID
		}
		return out.Conflicts[i].IMEI < out.Conflicts[j].IMEI
	})
	httpx.JSON(w, http.StatusOK, out)
}

// period is one side's row, normalised for comparison.
type period struct {
	imei      string
	vehicleMD string // master-data vehicle id, the only identifier both sides share
	plate     string
	from, to  time.Time // zero `to` = still fitted
}

func (p period) overlaps(q period) bool {
	endP, endQ := p.to, q.to
	if endP.IsZero() {
		endP = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	if endQ.IsZero() {
		endQ = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return p.from.Before(endQ) && q.from.Before(endP)
}

func (p period) show() HistoryPeriod {
	v := p.plate
	if v == "" {
		v = p.vehicleMD
	}
	hp := HistoryPeriod{IMEI: p.imei, Vehicle: v, From: p.from.Format(time.RFC3339)}
	if !p.to.IsZero() {
		hp.To = p.to.Format(time.RFC3339)
	}
	return hp
}

func (h *Handler) historyDiffTenant(ctx context.Context, t tenantRef, company string, from, to time.Time, out *HistoryDiff) error {
	up, err := h.mdRead.ListDeviceAssignments(ctx, company, "", from, to)
	if err != nil {
		return err
	}

	plateByMDID := map[string]string{}
	var mine []period
	err = db.Tx(ctx, h.pool, t.id, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT a.imei, coalesce(v.master_data_id,''), coalesce(v.license_plate,''), a.fitted_at, a.unfitted_at
			  FROM tracker_assignments a
			  JOIN vehicles v ON v.id = a.vehicle_id
			 WHERE a.fitted_at < $1 AND (a.unfitted_at IS NULL OR a.unfitted_at > $2)`, to, from)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p period
			var un *time.Time
			if err := rows.Scan(&p.imei, &p.vehicleMD, &p.plate, &p.from, &un); err != nil {
				return err
			}
			if un != nil {
				p.to = *un
			}
			mine = append(mine, p)
		}
		if err := rows.Err(); err != nil {
			return err
		}
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
		return err
	}

	theirs := make([]period, 0, len(up))
	for _, a := range up {
		theirs = append(theirs, period{imei: a.IMEI, vehicleMD: a.VehicleID, plate: plateByMDID[a.VehicleID], from: a.FittedAt, to: a.UnfittedAt})
	}

	matchedTheirs := make([]bool, len(theirs))
	for _, m := range mine {
		found := false
		for j, o := range theirs {
			if m.imei != o.imei || !m.overlaps(o) {
				continue
			}
			found = true
			matchedTheirs[j] = true
			if m.vehicleMD != "" && o.vehicleMD != "" && m.vehicleMD != o.vehicleMD {
				// The same device, at the same time, on two different trucks.
				out.Counts["conflict"]++
				out.Conflicts = append(out.Conflicts, HistoryConflict{
					TenantID: t.id, TenantName: t.name, IMEI: m.imei,
					Overlap: latest(m.from, o.from).Format(time.RFC3339),
					FMS:     m.show(), MasterData: o.show(),
				})
			} else {
				out.Counts["agreed"]++
			}
			break
		}
		if !found {
			// Master data has no row for this stretch — almost always
			// "never recorded upstream", not a contradiction.
			out.Counts["fms_only_not_recorded_upstream"]++
		}
	}
	for j, o := range theirs {
		if !matchedTheirs[j] {
			out.Counts["masterdata_only_not_recorded_here"]++
			_ = o
		}
	}
	return nil
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}
