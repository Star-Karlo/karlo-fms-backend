package vehicle

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/catalog"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
	"github.com/karlo/fms-backend/internal/store"
)

// Service holds vehicle business logic over the tenant-scoped store.
type Service struct {
	pool *db.Pool
	md   *mdwrite.Client   // nil before cutover: writes stay local
	cat  *catalog.Resolver // nil without master data: names go up unresolved (omitted)
	dir  mdwrite.CompanyResolver
}

// WithCatalog makes the write-through resolve manufacturer / box_type /
// vehicle_type to master data's brand / truck body / truck head ids, in the
// tenant's company scope (dir).
func (s *Service) WithCatalog(c *catalog.Resolver, dir mdwrite.CompanyResolver) *Service {
	s.cat, s.dir = c, dir
	return s
}

// groupMD resolves a local fleet group to its master-data id; "" when the
// group is unpaired (pre-backfill) or nil when no group is given.
func groupMD(ctx context.Context, q *store.Queries, fleetGroupID *int64) (*string, bool) {
	if fleetGroupID == nil {
		return nil, true
	}
	g, err := q.GetFleetGroup(ctx, *fleetGroupID)
	if err != nil || g.MasterDataID == nil {
		return nil, false
	}
	return g.MasterDataID, true
}

// mirrorGroup tells master data which group the vehicle is in (nil = none).
func (s *Service) mirrorGroup(ctx context.Context, caller mdwrite.Caller, localID int64, mdID string, group *string, resolved bool) {
	if !resolved {
		log.Printf("vehicle %d: fleet group set locally but not paired with master data; membership not sent", localID)
		return
	}
	if err := s.md.SetVehicleGroup(ctx, caller, mdID, group); err != nil {
		log.Printf("vehicle %d: fleet group set locally, master data %s rejected the membership: %v", localID, mdID, err)
	}
}

func (s *Service) company(ctx context.Context, tenantID int64) string {
	if s.dir == nil {
		return ""
	}
	c, _ := s.dir.CompanyForTenant(ctx, tenantID)
	return c
}

// catalogIDs resolves FMS's three free-text columns against the caller's
// company catalogues. vehicle_type is a HEAD only when it names one; a body
// or class word ("Wingbox", "Tronton") resolves through box_type's catalogue
// instead, so the id is sent as truckBodyId when box_type is empty.
func (s *Service) catalogIDs(ctx context.Context, company string, manufacturer, vehicleType, boxType *string) (brand, body, head string) {
	if s.cat == nil || company == "" {
		return "", "", ""
	}
	if manufacturer != nil {
		brand = s.cat.Resolve(ctx, company, mdclient.KindBrand, *manufacturer)
	}
	if boxType != nil {
		body = s.cat.Resolve(ctx, company, mdclient.KindTruckBody, *boxType)
	}
	if vehicleType != nil {
		head = s.cat.Resolve(ctx, company, mdclient.KindTruckHead, *vehicleType)
		if head == "" && body == "" && (boxType == nil || *boxType == "") {
			body = s.cat.Resolve(ctx, company, mdclient.KindTruckBody, *vehicleType)
		}
	}
	return brand, body, head
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

// validTollAndSize rejects implausible toll and dimension figures with a
// readable message, rather than letting the CHECK constraints from 0057 and
// 0062 surface as a raw violation.
//
// THE BOUNDS ARE NOT PEDANTRY. The legacy truck master this data will be
// seeded from holds dimensions on 96% of its rows at values including 23 m
// wide, 32 m tall and 1.232 kg — someone typing 1, 2, 3 into a form. Every one
// of those passes a "greater than zero" test, which is exactly how they
// survived to be found years later.
func validTollAndSize(axles, gol *int16, length, width, height, weight *float64) error {
	if axles != nil && (*axles < 2 || *axles > 12) {
		return fmt.Errorf("axles must be between 2 and 12")
	}
	if gol != nil && (*gol < 1 || *gol > 5) {
		return fmt.Errorf("toll class override must be between 1 and 5")
	}
	for _, c := range []struct {
		v      *float64
		lo, hi float64
		name   string
	}{
		{length, 2, 30, "length (m)"},
		{width, 1.2, 3.5, "width (m)"},
		{height, 1.2, 5, "height (m)"},
		{weight, 500, 80000, "max weight (kg)"},
	} {
		if c.v != nil && (*c.v < c.lo || *c.v > c.hi) {
			return fmt.Errorf("%s must be between %g and %g", c.name, c.lo, c.hi)
		}
	}
	return nil
}

func (s *Service) Create(ctx context.Context, tenantID int64, caller mdwrite.Caller, req CreateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
	}
	if err := validTollAndSize(req.Axles, req.GolonganOverride,
		req.LengthM, req.WidthM, req.HeightM, req.MaxWeightKg); err != nil {
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
		mv := mdVehicle(req.LicensePlate, req.HullNo, req.UnitYear, req.Status)
		mv.BrandID, mv.TruckBodyID, mv.TruckHeadID = s.catalogIDs(ctx, s.company(ctx, tenantID), req.Manufacturer, req.VehicleType, req.BoxType)
		// So a truck added in FMS arrives upstream complete, rather than
		// needing the same figures typed again on the other side.
		mv.OdometerKm, mv.HourmeterHours = req.Odometer, req.Hourmeter
		mv.FuelTankLiters, mv.FuelRatioKmpl = req.FuelTankLiters, req.FuelRatioKmpl
		mv.Notes = req.Notes
		id, err := s.md.CreateVehicle(ctx, caller, mv)
		if err != nil {
			return nil, err
		}
		if id != "" {
			mdID = &id
		}
	}

	var dto DTO
	var group *string
	var groupOK bool
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		group, groupOK = groupMD(ctx, store.New(tx), req.FleetGroupID)
		v, err := store.New(tx).CreateVehicle(ctx, store.CreateVehicleParams{
			TenantID: tenantID, LicensePlate: req.LicensePlate, Manufacturer: req.Manufacturer,
			VehicleType: req.VehicleType, BoxType: req.BoxType, UnitYear: req.UnitYear, HullNo: req.HullNo,
			FleetGroupID: req.FleetGroupID, Status: derefStatus(req.Status), Garage: req.Garage,
			Odometer: conv.Num(req.Odometer), Hourmeter: conv.Num(req.Hourmeter),
			FuelTankLiters: req.FuelTankLiters, FuelRatioKmpl: req.FuelRatioKmpl,
			Axles: req.Axles, GolonganOverride: req.GolonganOverride,
			LengthM: req.LengthM, WidthM: req.WidthM, HeightM: req.HeightM,
			MaxWeightKg: req.MaxWeightKg,
			TrackerID:   req.TrackerID, CurrentDriverID: req.CurrentDriverID, Notes: req.Notes,
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
		if req.FleetGroupID != nil {
			s.mirrorGroup(ctx, caller, dto.ID, *mdID, group, groupOK)
		}
	}
	return &dto, nil
}

// Update patches a vehicle's fields.
func (s *Service) Update(ctx context.Context, tenantID, id int64, caller mdwrite.Caller, req UpdateRequest) (*DTO, error) {
	if err := validFuel(req.FuelTankLiters, req.FuelRatioKmpl); err != nil {
		return nil, err
	}
	if err := validTollAndSize(req.Axles, req.GolonganOverride,
		req.LengthM, req.WidthM, req.HeightM, req.MaxWeightKg); err != nil {
		return nil, err
	}
	// Status in and out of 'archive' is a retirement, not a field edit, and
	// master data has to agree about it: the projection restores any local row
	// master data still lists as live, so an archive set only here goes back to
	// 'active' within minutes — and the truck reappears on the live map with
	// nothing to explain it. Routed through the same retire the delete uses.
	if req.Status != nil {
		switch moved, err := s.retireIfStatusMoved(ctx, tenantID, id, caller, *req.Status); {
		case err != nil:
			return nil, err
		case moved:
			req.Status = nil // already applied by Archive, and not for the mirror below
		}
	}
	var dto DTO
	var mdID *string
	var group *string
	var groupOK, groupTouched bool
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// Read the pairing in the same transaction as the write, so the
		// upstream call targets the record this row belongs to.
		if before, err := q.GetVehicleRow(ctx, id); err == nil {
			mdID = before.MasterDataID
			// fleet_group_id is a patch field: present and different = a move.
			if req.FleetGroupID != nil && (before.FleetGroupID == nil || *before.FleetGroupID != *req.FleetGroupID) {
				groupTouched = true
				group, groupOK = groupMD(ctx, q, req.FleetGroupID)
			}
		}
		v, err := q.UpdateVehicle(ctx, store.UpdateVehicleParams{
			ID: id, LicensePlate: req.LicensePlate, Manufacturer: req.Manufacturer,
			VehicleType: req.VehicleType, BoxType: req.BoxType, UnitYear: req.UnitYear, HullNo: req.HullNo,
			FleetGroupID: req.FleetGroupID, Status: req.Status, Garage: req.Garage,
			Odometer: conv.Num(req.Odometer), Hourmeter: conv.Num(req.Hourmeter),
			FuelTankLiters: req.FuelTankLiters, FuelRatioKmpl: req.FuelRatioKmpl, Notes: req.Notes,
			Axles: req.Axles, GolonganOverride: req.GolonganOverride,
			LengthM: req.LengthM, WidthM: req.WidthM, HeightM: req.HeightM,
			MaxWeightKg: req.MaxWeightKg,
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
	// The six below join the list because master data now carries them. Until
	// it did, an edit to a tank size here stayed here: vehiclesync could not
	// receive the field, so the two products kept divergent copies of every
	// one of them with nothing to reconcile them. An FMS edit is no longer a
	// local fact.
	touchesMD := req.LicensePlate != nil || req.HullNo != nil || req.UnitYear != nil || req.Status != nil ||
		req.Manufacturer != nil || req.VehicleType != nil || req.BoxType != nil ||
		req.Odometer != nil || req.Hourmeter != nil ||
		req.FuelTankLiters != nil || req.FuelRatioKmpl != nil || req.Notes != nil ||
		// WITHOUT THESE, an edit that sets ONLY an axle count would write the
		// local column and never reach master data — so TMS would not get the
		// one field it needs most from this form.
		req.Axles != nil || req.GolonganOverride != nil ||
		req.LengthM != nil || req.WidthM != nil || req.HeightM != nil ||
		req.MaxWeightKg != nil
	if s.md != nil && mdID != nil && touchesMD {
		v := mdwrite.Vehicle{}
		v.BrandID, v.TruckBodyID, v.TruckHeadID = s.catalogIDs(ctx, s.company(ctx, tenantID), req.Manufacturer, req.VehicleType, req.BoxType)
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
		// Passed straight through as pointers: nil means the caller did not
		// touch the field, and master data leaves an absent key alone. The
		// request's own fields are already *T for the same reason.
		//
		// Colour is absent on purpose — FMS's own API never exposed it, on
		// create or update, so there is nothing here to send. It therefore
		// travels master data -> FMS only, which vehiclesync now does.
		v.OdometerKm = req.Odometer
		v.HourmeterHours = req.Hourmeter
		v.FuelTankLiters = req.FuelTankLiters
		v.FuelRatioKmpl = req.FuelRatioKmpl
		v.Notes = req.Notes
		v.Axles = req.Axles
		v.GolonganOverride = req.GolonganOverride
		v.LengthM, v.WidthM, v.HeightM = req.LengthM, req.WidthM, req.HeightM
		v.MaxWeightKg = req.MaxWeightKg
		if err := s.md.UpdateVehicle(ctx, caller, *mdID, v); err != nil {
			// Local write is already committed and cannot be rolled back here,
			// but the caller must learn it did not reach TMS.
			log.Printf("vehicle %d: local update committed, master data %s rejected it: %v", id, *mdID, err)
			return &dto, err
		}
	}
	if s.md != nil && mdID != nil && groupTouched {
		s.mirrorGroup(ctx, caller, id, *mdID, group, groupOK)
	}
	return &dto, nil
}

// retireIfStatusMoved applies a status edit that crosses the archive line, and
// reports whether it did. Into 'archive' it archives (master data first, so a
// refusal changes nothing). Out of 'archive' it refuses: master data owns the
// register and offers no un-delete, so FMS turning the row back to 'active'
// would be undone by the next sweep — better to say where the restore belongs
// than to accept an edit that quietly reverts.
func (s *Service) retireIfStatusMoved(ctx context.Context, tenantID, id int64, caller mdwrite.Caller, next string) (bool, error) {
	var before string
	var paired bool
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		row, err := store.New(tx).GetVehicleRow(ctx, id)
		if err != nil {
			return err
		}
		before, paired = row.Status, row.MasterDataID != nil
		return nil
	}); err != nil {
		return false, err
	}
	switch {
	case next == before:
		return false, nil
	case next == "archive":
		return true, s.Archive(ctx, tenantID, id, caller)
	case before == "archive" && paired && s.md != nil:
		return false, fmt.Errorf("this vehicle is retired in master data; restore it there and the next sync brings it back — setting it %s here would be reversed within minutes", next)
	}
	return false, nil
}

// Deleting a truck happens in two steps, because "delete" means two
// different things to the person clicking it.
//
// The first click retires it: status = 'archive', out of live view, out of
// reports, but still there to explain last month's trips. The second click,
// on a truck already retired, removes it for good. Anything irreversible is
// therefore behind a deliberate second decision, taken on a row that already
// shows as retired.

// RemoveResult says which of the two steps ran, and what the second one took
// with it.
type RemoveResult struct {
	// Archived is true for the first step, Deleted for the second.
	Archived bool `json:"archived"`
	Deleted  bool `json:"deleted"`
	// Removed counts the dependent FMS rows a hard delete destroyed, by kind,
	// so the answer can say what was lost rather than just "ok".
	Removed map[string]int64 `json:"removed,omitempty"`
}

// Remove archives a vehicle, or — if it is already archived — deletes it.
func (s *Service) Remove(ctx context.Context, tenantID, id int64, caller mdwrite.Caller) (RemoveResult, error) {
	var status string
	var mdID *string
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		before, err := store.New(tx).GetVehicleRow(ctx, id)
		if err != nil {
			return err
		}
		status, mdID = before.Status, before.MasterDataID
		return nil
	}); err != nil {
		return RemoveResult{}, err
	}
	if status == "archive" {
		return s.hardDelete(ctx, tenantID, id, mdID, caller)
	}
	return RemoveResult{Archived: true}, s.Archive(ctx, tenantID, id, caller)
}

// Archive soft-deletes a vehicle (status = archive).
//
// Master data goes FIRST, and a refusal leaves nothing changed. The order
// matters more here than it looks: vehiclesync restores any local row master
// data still lists as live, so archiving locally while the upstream retire
// fails does not leave a half-done delete — it leaves a truck that comes back
// as 'active' minutes later, on the live map, with nothing to say why. The
// earlier order (local first) made exactly that outcome the failure mode.
func (s *Service) Archive(ctx context.Context, tenantID, id int64, caller mdwrite.Caller) error {
	var mdID *string
	if err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		before, err := store.New(tx).GetVehicleRow(ctx, id)
		if err != nil {
			return err
		}
		mdID = before.MasterDataID
		return nil
	}); err != nil {
		return err
	}
	if s.md != nil && mdID != nil {
		if err := s.md.DeleteVehicle(ctx, caller, *mdID); err != nil {
			return fmt.Errorf("master data would not retire this vehicle, so it has been left active (it would otherwise be restored by the next sync): %w", err)
		}
	}
	return db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return store.New(tx).ArchiveVehicle(ctx, id)
	})
}

// hardDelete removes an archived vehicle and the FMS rows that name it.
//
// Upstream is retired again first, not because it should not already be —
// archiving did it — but because a row archived by the projection sweep, or
// by a hand-edited status, may never have been retired there, and a delete
// that leaves master data holding the truck would see it re-created here on
// the next pass as a brand-new vehicle.
func (s *Service) hardDelete(ctx context.Context, tenantID, id int64, mdID *string, caller mdwrite.Caller) (RemoveResult, error) {
	if s.md != nil && mdID != nil {
		if err := s.md.DeleteVehicle(ctx, caller, *mdID); err != nil {
			log.Printf("vehicle %d: master data %s would not retire it before the hard delete: %v", id, *mdID, err)
			return RemoveResult{}, fmt.Errorf("master data still lists this vehicle, so deleting it here would only bring it back on the next sync: %w", err)
		}
	}
	out := RemoveResult{Deleted: true, Removed: map[string]int64{}}
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// The tracker first: trackers.vehicle_id has no ON DELETE clause, so
		// it would block the delete, and clearing it closes the open fitting
		// period (which the cascade then takes away with the rest).
		if err := q.ClearTrackerByVehicle(ctx, &id); err != nil {
			return err
		}
		// Nullable vehicle_id on the historical tables, hence the pointer.
		for _, step := range []struct {
			kind string
			fn   func() (int64, error)
		}{
			{"alerts", func() (int64, error) { return q.DeleteVehicleAlerts(ctx, &id) }},
			{"service_schedules", func() (int64, error) { return q.DeleteVehicleServiceSchedules(ctx, &id) }},
			{"score_events", func() (int64, error) { return q.DeleteVehicleScoreEvents(ctx, &id) }},
			{"sop_runs", func() (int64, error) { return q.DeleteVehicleSopRuns(ctx, id) }},
			{"documents", func() (int64, error) { return q.DeleteVehicleDocuments(ctx, id) }},
		} {
			n, err := step.fn()
			if err != nil {
				return err
			}
			if n > 0 {
				out.Removed[step.kind] = n
			}
		}
		n, err := q.DeleteVehicle(ctx, id)
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		return nil
	})
	if err != nil {
		return RemoveResult{}, err
	}
	log.Printf("vehicle %d hard-deleted from tenant %d: %v", id, tenantID, out.Removed)
	return out, nil
}

// AssignTracker binds/unbinds a tracker, keeping both sides of the link in sync.
//
// The fitting is master data's too (TMS's Trackers page reads it), so it is
// mirrored upstream as fit/unfit after the local write — as the caller.
// Master data closes the previous fitting on fit itself.
func (s *Service) AssignTracker(ctx context.Context, tenantID, id int64, caller mdwrite.Caller, trackerID *int64) (*DTO, error) {
	var dto DTO
	var vehicleMD, trackerMD, previousMD *string
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		if before, err := q.GetVehicleRow(ctx, id); err == nil {
			vehicleMD = before.MasterDataID
			if before.TrackerID != nil && (trackerID == nil || *before.TrackerID != *trackerID) {
				if prev, err := q.GetTracker(ctx, *before.TrackerID); err == nil {
					previousMD = prev.MasterDataID
				}
			}
		}
		if trackerID != nil {
			if t, err := q.GetTracker(ctx, *trackerID); err == nil {
				trackerMD = t.MasterDataID
			}
		}
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
	if s.md == nil {
		return &dto, nil
	}
	// Unbind: the previous device's fitting closes upstream. On a swap the
	// fit below closes it itself (one GPS per truck), so only the unbind
	// case calls unfit.
	if previousMD != nil && trackerID == nil {
		if err := s.md.UnfitTracker(ctx, caller, *previousMD); err != nil {
			log.Printf("vehicle %d: tracker unbound locally, master data %s rejected the unfit: %v", id, *previousMD, err)
		}
	}
	if trackerID != nil {
		switch {
		case trackerMD == nil || vehicleMD == nil:
			log.Printf("vehicle %d: tracker %d bound locally; not paired with master data (tracker %v, vehicle %v), master data not told", id, *trackerID, trackerMD != nil, vehicleMD != nil)
		default:
			if err := s.md.FitTracker(ctx, caller, *trackerMD, *vehicleMD); err != nil {
				log.Printf("vehicle %d: tracker bound locally, master data %s rejected the fit: %v", id, *trackerMD, err)
				return &dto, err
			}
		}
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
