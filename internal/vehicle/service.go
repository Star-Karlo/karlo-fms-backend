package vehicle

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/store"
)

// Service holds vehicle business logic over the tenant-scoped store.
type Service struct {
	pool *db.Pool
	md   *mdwrite.Client // nil before cutover: writes stay local
}

// NewService builds the vehicle service.
func NewService(pool *db.Pool) *Service { return &Service{pool: pool} }

// WithMasterData makes vehicle writes reach karlo_masterdata as well as
// fms_app.
//
// Until this existed, a vehicle created in FMS lived only here. TMS never saw
// it — the user created three trucks for DEMO ACCOUNT and Control Tower showed
// none — and because the row carried no master_data_id it was also outside
// vehiclesync's reach: not archived (the sweep skips unpaired rows on purpose)
// but never reconciled either, and liable to be adopted by plate and partially
// overwritten if that plate later appeared upstream.
func (s *Service) WithMasterData(md *mdwrite.Client) *Service { s.md = md; return s }

// mdVehicle maps an FMS vehicle onto master data's vocabulary.
//
// Only what master data OWNS travels: plate, chassis, year, status. Odometer,
// hourmeter, fuel figures, tracker and driver links, garage and notes are FMS's
// own — sending them would be claiming authority over fields master data does
// not model and TMS does not want.
func mdVehicle(plate string, hullNo *string, unitYear *int32, status *string) mdwrite.Vehicle {
	v := mdwrite.Vehicle{LicensePlate: plate}
	if hullNo != nil {
		v.ChassisNumber = *hullNo
	}
	if unitYear != nil {
		v.Year = *unitYear
	}
	if status != nil {
		v.Status = *status
	}
	return v
}

// List returns a page of vehicles plus the total count for the filter.
func (s *Service) List(ctx context.Context, tenantID int64, p httpx.ListParams, fleetGroupID *int64) ([]DTO, int64, error) {
	var out []DTO
	var total int64
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		rows, err := q.ListVehicles(ctx, store.ListVehiclesParams{
			Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), FleetGroupID: fleetGroupID,
			Lim: int32(p.Limit), Off: int32(p.Offset),
		})
		if err != nil {
			return err
		}
		out = make([]DTO, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromListRow(r))
		}
		total, err = q.CountVehicles(ctx, store.CountVehiclesParams{
			Q: conv.StrPtr(p.Q), Status: conv.StrPtr(p.Status), FleetGroupID: fleetGroupID,
		})
		return err
	})
	return out, total, err
}

// ListAcross aggregates vehicles across every tenant (platform admin), each read
// inside its own scope and tagged with its owning account. only, when set,
// restricts to one account. Paging applies per account then concatenates — fine
// for the account-filtered master-data list (usually one account at a time).
func (s *Service) ListAcross(ctx context.Context, only *int64, p httpx.ListParams, fleetGroupID *int64) ([]DTO, int64, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM tenants ORDER BY id`)
	if err != nil {
		return nil, 0, err
	}
	type tref struct {
		id   int64
		name string
	}
	var tenants []tref
	for rows.Next() {
		var t tref
		if err := rows.Scan(&t.id, &t.name); err != nil {
			rows.Close()
			return nil, 0, err
		}
		tenants = append(tenants, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	out := []DTO{}
	var total int64
	for _, t := range tenants {
		if only != nil && *only != t.id {
			continue
		}
		items, cnt, err := s.List(ctx, t.id, p, fleetGroupID)
		if err != nil {
			continue
		}
		tid, name := t.id, t.name
		for i := range items {
			items[i].TenantID = &tid
			items[i].TenantName = &name
		}
		out = append(out, items...)
		total += cnt
	}
	return out, total, nil
}

// Get returns one vehicle with joined labels.
func (s *Service) Get(ctx context.Context, tenantID, id int64) (*DTO, error) {
	var dto DTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		row, err := store.New(tx).GetVehicle(ctx, id)
		if err != nil {
			return err
		}
		dto = fromGetRow(row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// Create inserts a vehicle.

// validFuel rejects non-positive fuel figures with a readable message rather
// than letting the DB CHECK surface a raw constraint error to the client. A
// zero ratio is also a divide-by-zero in every downstream estimate.
func validFuel(tank, ratio *float64) error {
	if tank != nil && *tank <= 0 {
		return fmt.Errorf("fuel tank size must be greater than 0")
	}
	if ratio != nil && *ratio <= 0 {
		return fmt.Errorf("fuel ratio (km/L) must be greater than 0")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, tenantID int64, caller mdwrite.Caller, req CreateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
	}

	// MASTER DATA FIRST, so the local row is born paired.
	//
	// Master data owns the fleet register; fms_app.vehicles is a projection of
	// it. Creating upstream first means master_data_id is known before the
	// INSERT, so the vehicle is visible to TMS and inside vehiclesync's reach
	// from the moment it exists. Creating locally first would produce exactly
	// the rows this change is cleaning up: present in FMS, absent from TMS, and
	// unreachable by the projection because it skips unpaired rows.
	//
	// If master data refuses, nothing is created anywhere and the caller is told
	// why — including a 403, which means the user genuinely may not add vehicles
	// upstream and is the correct answer rather than a bug to work around.
	var mdID *string
	if s.md != nil {
		id, err := s.md.CreateVehicle(ctx, caller, mdVehicle(
			req.LicensePlate, req.HullNo, req.UnitYear, req.Status,
		))
		if err != nil {
			return nil, err
		}
		if id != "" {
			mdID = &id
		}
	}

	var dto DTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		v, err := store.New(tx).CreateVehicle(ctx, store.CreateVehicleParams{
			TenantID: tenantID, LicensePlate: req.LicensePlate, Manufacturer: req.Manufacturer,
			VehicleType: req.VehicleType, BoxType: req.BoxType, UnitYear: req.UnitYear, HullNo: req.HullNo,
			FleetGroupID: req.FleetGroupID, Status: derefStatus(req.Status), Garage: req.Garage,
			Odometer: conv.Num(req.Odometer), Hourmeter: conv.Num(req.Hourmeter),
			FuelTankLiters: req.FuelTankLiters, FuelRatioKmpl: req.FuelRatioKmpl,
			TrackerID: req.TrackerID, CurrentDriverID: req.CurrentDriverID, Notes: req.Notes,
			MasterDataID: mdID,
		})
		if err != nil {
			return err
		}
		dto = fromVehicle(v)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Point master data back at the FMS row. Best-effort: the vehicle already
	// works in both products without it, so failing the create here would
	// report an error for something that succeeded. The backfill re-stamps.
	if s.md != nil && mdID != nil {
		if err := s.md.StampVehicleFMSID(ctx, caller, *mdID, dto.ID); err != nil {
			log.Printf("vehicle: created %d / master data %s but could not stamp fmsVehicleId: %v", dto.ID, *mdID, err)
		}
	}
	return &dto, nil
}

// Update patches a vehicle's fields.
func (s *Service) Update(ctx context.Context, tenantID, id int64, caller mdwrite.Caller, req UpdateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
	}
	var dto DTO
	var mdID *string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// Read the pairing in the same transaction as the write, so the
		// upstream call targets the record this row belongs to.
		if before, err := q.GetVehicleRow(ctx, id); err == nil {
			mdID = before.MasterDataID
		}
		v, err := q.UpdateVehicle(ctx, store.UpdateVehicleParams{
			ID: id, LicensePlate: req.LicensePlate, Manufacturer: req.Manufacturer,
			VehicleType: req.VehicleType, BoxType: req.BoxType, UnitYear: req.UnitYear, HullNo: req.HullNo,
			FleetGroupID: req.FleetGroupID, Status: req.Status, Garage: req.Garage,
			Odometer: conv.Num(req.Odometer), Hourmeter: conv.Num(req.Hourmeter),
			FuelTankLiters: req.FuelTankLiters, FuelRatioKmpl: req.FuelRatioKmpl, Notes: req.Notes,
		})
		if err != nil {
			return err
		}
		dto = fromVehicle(v)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Mirror the master-data-owned fields upstream. Only when the edit actually
	// touched one — an odometer correction is FMS's own business and has no
	// counterpart there, so sending it would be a write with nothing in it.
	//
	// An UNPAIRED vehicle is skipped, not created: it predates write-through and
	// belongs to the backfill, which handles the whole set deliberately rather
	// than racing it one edit at a time.
	touchesMD := req.LicensePlate != nil || req.HullNo != nil || req.UnitYear != nil || req.Status != nil
	if s.md != nil && mdID != nil && touchesMD {
		v := mdwrite.Vehicle{}
		if req.LicensePlate != nil {
			v.LicensePlate = *req.LicensePlate
		}
		if req.HullNo != nil {
			v.ChassisNumber = *req.HullNo
		}
		if req.UnitYear != nil {
			v.Year = *req.UnitYear
		}
		if req.Status != nil {
			v.Status = *req.Status
		}
		if err := s.md.UpdateVehicle(ctx, caller, *mdID, v); err != nil {
			// Local write is already committed and cannot be rolled back here,
			// but the caller must learn it did not reach TMS.
			log.Printf("vehicle %d: local update committed, master data %s rejected it: %v", id, *mdID, err)
			return &dto, err
		}
	}
	return &dto, nil
}

// Archive soft-deletes a vehicle (status = archive).
func (s *Service) Archive(ctx context.Context, tenantID, id int64, caller mdwrite.Caller) error {
	var mdID *string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		if before, err := q.GetVehicleRow(ctx, id); err == nil {
			mdID = before.MasterDataID
		}
		return q.ArchiveVehicle(ctx, id)
	})
	if err != nil {
		return err
	}
	// Retire it upstream too. Without this vehiclesync un-archives it on the
	// next pass — step 3b restores any local row master data still lists as
	// live — so the truck would reappear minutes after being archived, with no
	// indication why.
	if s.md != nil && mdID != nil {
		if err := s.md.DeleteVehicle(ctx, caller, *mdID); err != nil {
			log.Printf("vehicle %d: archived locally, master data %s still lists it live: %v", id, *mdID, err)
			return err
		}
	}
	return nil
}

// AssignTracker binds/unbinds a tracker, keeping both sides of the link in sync.
func (s *Service) AssignTracker(ctx context.Context, tenantID, id int64, trackerID *int64) (*DTO, error) {
	var dto DTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		v, err := q.AssignVehicleTracker(ctx, store.AssignVehicleTrackerParams{ID: id, TrackerID: trackerID})
		if err != nil {
			return err
		}
		// Keep trackers.vehicle_id (the assignment-history trigger's source of
		// truth) consistent on BOTH bind and unbind:
		//   unbind → detach whatever tracker is on this vehicle (closes its period);
		//   bind   → detach any OTHER tracker on this vehicle, then attach the chosen
		//            one (so the vehicle never has two open device assignments).
		if trackerID == nil {
			if err := q.ClearTrackerByVehicle(ctx, &id); err != nil {
				return err
			}
		} else {
			if err := q.ClearOtherTrackersOnVehicle(ctx, store.ClearOtherTrackersOnVehicleParams{VehicleID: &id, ID: *trackerID}); err != nil {
				return err
			}
			if _, err := q.SetTrackerVehicle(ctx, store.SetTrackerVehicleParams{ID: *trackerID, VehicleID: &id}); err != nil {
				return err
			}
		}
		dto = fromVehicle(v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &dto, nil
}

// AssignDriver sets a vehicle's current driver, here and in master data.
//
// The pairing is master data's (TMS reads it for trips), so it is mirrored
// upstream as currentDriverId the way plate/status edits are — as the
// caller, after the local write. A driver with no master_data_id cannot be
// named upstream; the local assignment stands and the caller is told.
func (s *Service) AssignDriver(ctx context.Context, tenantID, id int64, caller mdwrite.Caller, driverID *int64) (*DTO, error) {
	var dto DTO
	var mdID, driverMD *string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		if driverID != nil {
			e, err := q.GetEmployee(ctx, *driverID)
			if err != nil {
				return fmt.Errorf("driver %d: %w", *driverID, err)
			}
			driverMD = e.MasterDataID
		}
		v, err := q.AssignVehicleDriver(ctx, store.AssignVehicleDriverParams{ID: id, CurrentDriverID: driverID})
		if err != nil {
			return err
		}
		mdID = v.MasterDataID
		dto = fromVehicle(v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.md == nil || mdID == nil {
		return &dto, nil
	}
	if driverID != nil && driverMD == nil {
		log.Printf("vehicle %d: driver %d assigned locally but has no master_data_id; master data not told", id, *driverID)
		return &dto, fmt.Errorf("driver is not paired with master data; assigned in FMS only")
	}
	if err := s.md.SetVehicleDriver(ctx, caller, *mdID, driverMD); err != nil {
		log.Printf("vehicle %d: driver assignment committed locally, master data %s rejected it: %v", id, *mdID, err)
		return &dto, err
	}
	return &dto, nil
}

// derefStatus adapts an optional status into the interface{} the generated
// COALESCE param expects (nil ⇒ DB default 'active').
func derefStatus(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}
