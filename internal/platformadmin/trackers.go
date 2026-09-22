package platformadmin

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// The tracker registry, fleet-wide, for the master-data reconcile.
//
// Devices were the one dataset the cutover missed: FMS kept its own
// fms_app.trackers (+ tracker_assignments for the fit history) and
// fms_app.dashcams, master data grew its own trackers/tracker_assignments
// from a TMS backfill, and nothing kept the two in step. Positions flow from
// FMS's registry, so it is the truth master data is being aligned TO — this
// export is what master data's owner reads to re-own each device to its
// company and set its current fitting. After that, master data owns devices
// and FMS projects them (the same shape as vehicles and drivers).

// TrackerExport is one device as FMS knows it, keyed the way master data is.
type TrackerExport struct {
	Kind       string `json:"kind"` // gps | dashcam
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	// CompanyID is the master-data company (UUID), when the directory knows it.
	CompanyID string `json:"company_id,omitempty"`
	// DeviceID is the IMEI for a GPS unit, the VSS device id for a dashcam.
	DeviceID    string  `json:"device_id"`
	PhoneNo     *string `json:"phone_no,omitempty"`
	Status      string  `json:"status"`
	ExternalRef *string `json:"external_ref,omitempty"`
	DeviceName  *string `json:"device_name,omitempty"`
	DeviceType  *string `json:"device_type,omitempty"`
	// Current fitting: nil when the device is in stock.
	VehicleID           *int64     `json:"vehicle_id,omitempty"`
	VehicleMasterDataID *string    `json:"vehicle_master_data_id,omitempty"`
	LicensePlate        *string    `json:"license_plate,omitempty"`
	FittedAt            *time.Time `json:"fitted_at,omitempty"`
	// HasAssignmentHistory: fitted_at came from a real tracker_assignments
	// row. False means the device was imported already on the vehicle and
	// fitted_at is its created_at — "at least since then", not an install date.
	HasAssignmentHistory bool `json:"has_assignment_history"`
	// IsDemo: a copy of a device owned by another account, shown here for a
	// demo. Master data keeps the real owner; skip this row when reconciling.
	IsDemo    bool      `json:"is_demo"`
	CreatedAt time.Time `json:"created_at"`
}

// trackersExport godoc
//
//	@Summary		Export the tracker and dashcam registry, every account (platform admin)
//	@Description	FMS's own device registry as one list: GPS trackers (kind=gps, device_id=IMEI, phone_no, status, external_ref) and dashcams (kind=dashcam, device_id=VSS device id), each with its account, the account's master-data company id, and the current fitting (vehicle, its master-data id, plate, fitted_at from the open tracker_assignments row — has_assignment_history=true — else the tracker's created_at as an estimate). Devices in stock are included with no vehicle fields. For the one-off reconcile that makes master data the owner of devices; restricted to platform_admin.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	httpx.List[platformadmin.TrackerExport]
//	@Failure		403	{object}	map[string]string
//	@Router			/v1/platform/trackers/export [get]
func (h *Handler) trackersExport(w http.ResponseWriter, r *http.Request) {
	tenants, err := h.tenantList(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list accounts failed")
		return
	}
	items := make([]TrackerExport, 0, 2048)
	for _, t := range tenants {
		rows, err := h.exportTenant(r.Context(), t)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "export failed for "+t.name+": "+err.Error())
			return
		}
		items = append(items, rows...)
	}
	httpx.JSON(w, http.StatusOK, httpx.List[TrackerExport]{Items: items, Total: int64(len(items))})
}

// exportTenant reads one account's devices as that tenant (RLS applies).
func (h *Handler) exportTenant(ctx context.Context, t tenantRef) ([]TrackerExport, error) {
	company := ""
	if h.dir != nil {
		company, _ = h.dir.CompanyForTenant(ctx, t.id)
	}
	var out []TrackerExport
	err := db.Tx(ctx, h.pool, t.id, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT tr.imei, tr.phone_no, tr.status, tr.external_ref, tr.created_at, tr.is_demo,
			       v.id, v.master_data_id, v.license_plate,
			       (SELECT max(a.fitted_at) FROM tracker_assignments a
			         WHERE a.tracker_id = tr.id AND a.unfitted_at IS NULL)
			  FROM trackers tr
			  LEFT JOIN vehicles v ON v.id = tr.vehicle_id
			 ORDER BY tr.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := TrackerExport{Kind: "gps", TenantID: t.id, TenantName: t.name, CompanyID: company}
			if err := rows.Scan(&e.DeviceID, &e.PhoneNo, &e.Status, &e.ExternalRef, &e.CreatedAt, &e.IsDemo,
				&e.VehicleID, &e.VehicleMasterDataID, &e.LicensePlate, &e.FittedAt); err != nil {
				return err
			}
			e.HasAssignmentHistory = e.FittedAt != nil
			if e.VehicleID != nil && e.FittedAt == nil {
				// Fitted before the assignment history existed (migration 0040).
				c := e.CreatedAt
				e.FittedAt = &c
			}
			out = append(out, e)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT d.vss_device_id, d.device_name, d.device_type, d.created_at, d.is_demo,
			       v.id, v.master_data_id, v.license_plate
			  FROM dashcams d
			  LEFT JOIN vehicles v ON v.id = d.vehicle_id
			 ORDER BY d.id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			e := TrackerExport{Kind: "dashcam", TenantID: t.id, TenantName: t.name, CompanyID: company, Status: "active"}
			if err := rows.Scan(&e.DeviceID, &e.DeviceName, &e.DeviceType, &e.CreatedAt, &e.IsDemo,
				&e.VehicleID, &e.VehicleMasterDataID, &e.LicensePlate); err != nil {
				return err
			}
			if e.VehicleID != nil {
				c := e.CreatedAt
				e.FittedAt = &c
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
