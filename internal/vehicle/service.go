package vehicle

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Service holds vehicle business logic over the tenant-scoped store.
type Service struct{ pool *db.Pool }

// NewService builds the vehicle service.
func NewService(pool *db.Pool) *Service { return &Service{pool: pool} }

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

func (s *Service) Create(ctx context.Context, tenantID int64, req CreateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
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
	return &dto, nil
}

// Update patches a vehicle's fields.
func (s *Service) Update(ctx context.Context, tenantID, id int64, req UpdateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
	}
	var dto DTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		v, err := store.New(tx).UpdateVehicle(ctx, store.UpdateVehicleParams{
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
	return &dto, nil
}

// Archive soft-deletes a vehicle (status = archive).
func (s *Service) Archive(ctx context.Context, tenantID, id int64) error {
	return db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return store.New(tx).ArchiveVehicle(ctx, id)
	})
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
		if trackerID != nil {
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

// AssignDriver sets a vehicle's current driver.
func (s *Service) AssignDriver(ctx context.Context, tenantID, id int64, driverID *int64) (*DTO, error) {
	var dto DTO
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		v, err := store.New(tx).AssignVehicleDriver(ctx, store.AssignVehicleDriverParams{ID: id, CurrentDriverID: driverID})
		if err != nil {
			return err
		}
		dto = fromVehicle(v)
		return nil
	})
	if err != nil {
		return nil, err
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
