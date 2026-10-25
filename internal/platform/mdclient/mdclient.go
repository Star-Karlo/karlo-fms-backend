// Package mdclient is this service's gRPC connection to the shared master-data
// service.
//
// One caller today: vehiclesync, which projects the fleet register into
// fms_app.vehicles so the sixteen queries that join on the bigint keep working.
//
// SERVICE TOKEN, NOT A USER TOKEN. The sync runs in a background loop with no
// request behind it, so there is no caller's credential to forward — the same
// reasoning as iamclient. Borrowing a user's token would tie the whole fleet
// projection to one person's account and stop the day they leave.
package mdclient

import (
	"context"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"

	"google.golang.org/grpc"

	commonv1 "github.com/karlo/fms-backend/internal/platform/genproto/karlo/common/v1"
	mdv1 "github.com/karlo/fms-backend/internal/platform/genproto/karlo/masterdata/v1"
	"github.com/karlo/fms-backend/internal/platform/grpcutil"
)

// Client talks to the master-data service.
type Client struct {
	conn   *grpc.ClientConn
	client mdv1.MasterDataServiceClient
}

// New dials master data, or returns nil when no target is configured — the
// state of a deployment that has not cut over.
func New(target, serviceName, serviceToken string) (*Client, error) {
	if target == "" {
		return nil, nil
	}
	conn, err := grpcutil.Dial(grpcutil.DialConfig{
		Service:      serviceName,
		Target:       target,
		ServiceToken: serviceToken,
	})
	if err != nil {
		return nil, fmt.Errorf("mdclient: %w", err)
	}
	return &Client{conn: conn, client: mdv1.NewMasterDataServiceClient(conn)}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Vehicle is the subset of a master-data truck this service projects.
type Vehicle struct {
	ID            string // master data's ObjectId — the stable key
	CompanyID     string
	LicensePlate  string
	ChassisNumber string
	Year          int32
	Status        string
	IMEI          string
	Deleted       bool
	// DriverIDs are the drivers master data has paired with the truck —
	// today at most one (currentDriverId). Empty means unpaired.
	DriverIDs []string
	// Catalogue references (brand, truck body, truck head). Names come from
	// ListCatalogItems; FMS stores the names.
	BrandID, TruckBodyID, TruckHeadID string
	// TruckGroupID is the vehicle group (FMS: fleet group); empty = none.
	TruckGroupID string

	/* POINTERS, where every field above is a value.

	   The contract declares these `optional` and the difference is load
	   bearing here, not upstream: vehiclesync projects master data over
	   fms_app.vehicles, and it has to tell "master data says 340 litres" from
	   "master data knows nothing about this truck's tank". Both arrive as 0.0
	   if presence is dropped at this boundary, and the projection would then
	   write zeroes over figures operators entered in FMS — across the whole
	   fleet, on the first pass, silently.

	   So nil means "upstream has no opinion, keep what is here". */
	Color          *string
	OdometerKm     *float64
	HourmeterHours *float64
	FuelTankLiters *float64
	FuelRatioKmpl  *float64
	Notes          *string

	/* TOLL AND DIMENSIONS, pending in master data.

	   Axles is the key every Indonesian toll tariff is priced on — golongan
	   II at two axles through V at five or more — and FMS has nowhere else to
	   get it. The legacy truck master in Mongo carries one on 53% of live
	   rows and some of those contradict the vehicle (axle 3 beside a
	   "Pickup_2_other" icon), so it can seed a column and cannot be the
	   source of truth.

	   THE REASON THIS BELONGS UPSTREAM AND NOT IN FMS. The toll spend being
	   addressed is TMS order quoting, not FMS maintenance. A value entered in
	   FMS reaches nothing else: vehiclesync projects master data ONTO
	   fms_app.vehicles and nothing writes back. Entered in master data, both
	   products get it from one place.

	   Nil keeps whatever FMS holds, like every pointer above — so an operator
	   entering an axle count in FMS today is not overwritten, and master data
	   takes over the moment it starts sending one. */
	Axles *int16
	// LengthM, WidthM, HeightM and MaxWeightKg are what Valhalla needs to
	// apply the height, width and weight restrictions it already knows from
	// OpenStreetMap. Without them every route uses fleet-wide defaults, so
	// a 40ft trailer is routed like a van. The legacy data has these on 96%
	// of trucks at values including 23 m wide and 32 m tall, which is why
	// they are wanted from a source someone is accountable for.
	LengthM     *float64
	WidthM      *float64
	HeightM     *float64
	MaxWeightKg *float64
}

// DeviceAssignment is one stretch during which a vehicle carried a device.
// UnfittedAt is the zero time while the device is still fitted — master data
// deliberately does not clamp it to the window's end, so "still on the truck"
// stays distinguishable from "removed exactly then".
type DeviceAssignment struct {
	VehicleID  string // master-data vehicle id
	IMEI       string
	FittedAt   time.Time
	UnfittedAt time.Time
}

// ListDeviceAssignments answers which device a vehicle carried, and when,
// over a window — the question a historical read has to ask, because a
// reading is keyed by IMEI and a box moves between trucks.
//
// An empty vehicleID asks for the WHOLE company, which is how this service
// reconciles its own copy: one call rather than one per vehicle. Both ends
// of the window are required upstream; the wrong answer to a loose query is
// not an error but a plausible device belonging to another truck.
func (c *Client) ListDeviceAssignments(ctx context.Context, companyID, vehicleID string, from, to time.Time) ([]DeviceAssignment, error) {
	resp, err := c.client.ListDeviceAssignments(ctx, &mdv1.ListDeviceAssignmentsRequest{
		CompanyId: companyID,
		VehicleId: vehicleID,
		From:      timestamppb.New(from),
		To:        timestamppb.New(to),
	})
	if err != nil {
		return nil, fmt.Errorf("mdclient: device assignments for %s: %w", companyID, err)
	}
	out := make([]DeviceAssignment, 0, len(resp.GetAssignments()))
	for _, a := range resp.GetAssignments() {
		d := DeviceAssignment{VehicleID: a.GetVehicleId(), IMEI: a.GetImei(), FittedAt: a.GetFittedAt().AsTime()}
		if a.GetUnfittedAt() != nil {
			d.UnfittedAt = a.GetUnfittedAt().AsTime()
		}
		out = append(out, d)
	}
	return out, nil
}

// TruckGroup is a company's vehicle group (FMS: fleet group).
type TruckGroup struct {
	ID          string
	CompanyID   string
	Name        string
	Description string
	Deleted     bool
}

// ListTruckGroups returns every group of a company, retired ones included.
func (c *Client) ListTruckGroups(ctx context.Context, companyID string) ([]TruckGroup, error) {
	resp, err := c.client.ListTruckGroups(ctx, &mdv1.ListTruckGroupsRequest{CompanyId: companyID})
	if err != nil {
		return nil, fmt.Errorf("mdclient: list truck groups for %s: %w", companyID, err)
	}
	out := make([]TruckGroup, 0, len(resp.GetGroups()))
	for _, g := range resp.GetGroups() {
		out = append(out, TruckGroup{ID: g.GetId(), CompanyID: g.GetCompanyId(), Name: g.GetName(), Description: g.GetDescription(), Deleted: g.GetDeleted()})
	}
	return out, nil
}

// CatalogKind names the catalogues FMS reads and resolves against.
type CatalogKind int32

const (
	KindBrand     CatalogKind = CatalogKind(mdv1.CatalogKind_CATALOG_KIND_BRAND)
	KindTruckBody CatalogKind = CatalogKind(mdv1.CatalogKind_CATALOG_KIND_TRUCK_BODY)
	KindTruckHead CatalogKind = CatalogKind(mdv1.CatalogKind_CATALOG_KIND_TRUCK_HEAD)
)

// CatalogItem is one catalogue entry: Karlo's global one (CompanyID empty)
// or a company's own.
type CatalogItem struct {
	ID        string
	Name      string
	CompanyID string
	Active    bool
}

// ListCatalogItems returns the globals plus the company's own entries for a
// catalogue, paged through.
func (c *Client) ListCatalogItems(ctx context.Context, kind CatalogKind, companyID string) ([]CatalogItem, error) {
	var out []CatalogItem
	for page := int32(0); ; page++ {
		resp, err := c.client.ListCatalogItems(ctx, &mdv1.ListCatalogItemsRequest{
			Kind:      mdv1.CatalogKind(kind),
			CompanyId: companyID,
			Query: &commonv1.Query{
				Page: &commonv1.Page{Page: page, PageSize: pageSize},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("mdclient: list catalog %d for %q: %w", kind, companyID, err)
		}
		items := resp.GetItems()
		for _, it := range items {
			out = append(out, CatalogItem{ID: it.GetId(), Name: it.GetName(), CompanyID: it.GetCompanyId(), Active: it.GetActive()})
		}
		info := resp.GetPageInfo()
		if len(items) < pageSize || (info != nil && int64(len(out)) >= info.GetTotalRows()) {
			return out, nil
		}
	}
}

// pageSize is what master data caps a page at. Asking for more is not an error
// there; it simply returns fewer, which would make a naive "got what I asked
// for, so that is everything" check wrong.
const pageSize = 200

// ListVehicles returns every vehicle a company owns, paged to exhaustion.
//
// EXHAUSTION MATTERS MORE HERE THAN ALMOST ANYWHERE. A truncated read does not
// look like a failure: the projection would simply not contain the trucks past
// the first page, and every alert against one of them would render with no
// plate. Siba Surya alone has 1,803 vehicles — nine pages.
//
// Deleted vehicles are included on purpose. The projection has to learn that a
// truck is gone, and a list that silently omits them would leave the local row
// active forever.
func (c *Client) ListVehicles(ctx context.Context, companyID string) ([]Vehicle, error) {
	var out []Vehicle

	for page := int32(0); ; page++ {
		resp, err := c.client.ListTrucks(ctx, &mdv1.ListTrucksRequest{
			CompanyId: companyID,
			Query: &commonv1.Query{
				Page: &commonv1.Page{Page: page, PageSize: pageSize},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("mdclient: list trucks for %s: %w", companyID, err)
		}
		trucks := resp.GetTrucks()
		for _, t := range trucks {
			out = append(out, Vehicle{
				ID:            t.GetId(),
				CompanyID:     t.GetCompanyId(),
				LicensePlate:  t.GetPoliceNumber(),
				ChassisNumber: t.GetChassisNumber(),
				Year:          t.GetYear(),
				Status:        t.GetStatus(),
				IMEI:          t.GetImei(),
				Deleted:       t.GetDeleted(),
				DriverIDs:     t.GetDriverIds(),
				BrandID:       t.GetBrandId(),
				TruckBodyID:   t.GetTruckBodyId(),
				TruckHeadID:   t.GetTruckHeadId(),
				TruckGroupID:  t.GetTruckGroupId(),
				// NOT GetX(): the generated getters return the zero value for
				// an absent optional, which is exactly the distinction the
				// field comments above exist to keep. Read the pointers.
				Color:          t.Color,
				OdometerKm:     t.OdometerKm,
				HourmeterHours: t.HourmeterHours,
				FuelTankLiters: t.FuelTankLiters,
				FuelRatioKmpl:  t.FuelRatioKmpl,
				Notes:          t.Notes,
				// The toll key and the routing dimensions. These fields have
				// been read by vehiclesync since it was written — its UPDATE
				// COALESCEs over them — and were always nil because the
				// contract did not carry them. Master data now sends them, so
				// that statement stops being a no-op.
				Axles:       axles16(t.Axles),
				LengthM:     t.LengthM,
				WidthM:      t.WidthM,
				HeightM:     t.HeightM,
				MaxWeightKg: t.MaxWeightKg,
			})
		}

		// Stop on a short page OR on having reached the reported total —
		// either alone can be wrong. A page exactly filling the last slot
		// makes the short-page test miss, and total_rows can lag behind a
		// concurrent write; requiring both to disagree before continuing is
		// what stops this looping forever on a server that always returns a
		// full page.
		info := resp.GetPageInfo()
		if len(trucks) < pageSize || (info != nil && int64(len(out)) >= info.GetTotalRows()) {
			return out, nil
		}
	}
}

// Tracker is a master-data device: a GPS unit (DeviceID = IMEI) or a
// dashcam (DeviceID = VSS device id).
type Tracker struct {
	ID               string
	CompanyID        string
	Kind             string // gps | dashcam
	DeviceID         string
	PhoneNo          string
	Status           string // active | inactive | retired
	CurrentVehicleID string // master-data vehicle id; empty when in stock
	FittedAt         time.Time
	Deleted          bool
}

// ListTrackers pages every device master data holds for a company (both
// kinds when kind is empty).
func (c *Client) ListTrackers(ctx context.Context, companyID, kind string) ([]Tracker, error) {
	var out []Tracker
	for page := int32(0); ; page++ {
		resp, err := c.client.ListTrackers(ctx, &mdv1.ListTrackersRequest{
			CompanyId: companyID,
			Kind:      kind,
			Query: &commonv1.Query{
				Page: &commonv1.Page{Page: page, PageSize: pageSize},
			},
		})
		if err != nil {
			return nil, fmt.Errorf("mdclient: list trackers for %s: %w", companyID, err)
		}
		items := resp.GetTrackers()
		for _, t := range items {
			out = append(out, Tracker{
				ID:               t.GetId(),
				CompanyID:        t.GetCompanyId(),
				Kind:             t.GetKind(),
				DeviceID:         t.GetDeviceId(),
				PhoneNo:          t.GetPhoneNo(),
				Status:           t.GetStatus(),
				CurrentVehicleID: t.GetCurrentVehicleId(),
				FittedAt:         t.GetFittedAt().AsTime(),
				Deleted:          t.GetDeleted(),
			})
		}
		// Same stop rule as ListVehicles.
		info := resp.GetPageInfo()
		if len(items) < pageSize || (info != nil && int64(len(out)) >= info.GetTotalRows()) {
			return out, nil
		}
	}
}

// Driver is the subset of a master-data driver that FMS projects.
type Driver struct {
	ID            string // master data's ObjectId — the stable key
	CompanyID     string
	FullName      string
	Phone         string
	EmployeeNo    string
	LicenseNo     string
	LicenseClass  string
	LicenseExpiry *time.Time
	Status        string
	// FMSEmployeeID is the bigint master data believes this driver has in FMS,
	// 0 when it has never been told one. It is a hint for adoption, not an
	// authority: the local master_data_id is what the projection matches on.
	FMSEmployeeID int64
	Deleted       bool
}

// ListDrivers returns every driver a company has, paged to exhaustion.
//
// The same exhaustion argument as ListVehicles, and the same reason for
// including deleted rows: the projection has to learn that someone has left,
// or their local record stays active and assignable forever. Siba Surya alone
// has 1,385 drivers — seven pages.
func (c *Client) ListDrivers(ctx context.Context, companyID string) ([]Driver, error) {
	var out []Driver

	for page := int32(0); ; page++ {
		resp, err := c.client.ListDrivers(ctx, &mdv1.ListDriversRequest{
			CompanyId: companyID,
			Page:      page,
			PageSize:  pageSize,
		})
		if err != nil {
			return nil, fmt.Errorf("mdclient: list drivers for %s: %w", companyID, err)
		}
		drivers := resp.GetDrivers()
		for _, d := range drivers {
			dr := Driver{
				ID:            d.GetId(),
				CompanyID:     d.GetCompanyId(),
				FullName:      d.GetFullName(),
				Phone:         d.GetPhone(),
				EmployeeNo:    d.GetEmployeeNo(),
				LicenseNo:     d.GetLicenseNo(),
				LicenseClass:  d.GetLicenseClass(),
				Status:        d.GetStatus(),
				FMSEmployeeID: d.GetFmsEmployeeId(),
				Deleted:       d.GetDeleted(),
			}
			if ts := d.GetLicenseExpiry(); ts != nil {
				t := ts.AsTime()
				dr.LicenseExpiry = &t
			}
			out = append(out, dr)
		}

		// Both conditions, for the reason spelled out in ListVehicles: a page
		// that exactly fills defeats the short-page test, and a lagging total
		// defeats the count test.
		if len(drivers) < pageSize || int64(len(out)) >= resp.GetTotal() {
			return out, nil
		}
	}
}

// axles16 narrows the wire's int32 to the column's int16, keeping absence.
//
// Absence is the point: nil means the axle count was never recorded, and
// vehiclesync COALESCEs this field — so a zero would erase a count an
// operator entered in FMS rather than leave it alone.
func axles16(n *int32) *int16 {
	if n == nil {
		return nil
	}
	v := int16(*n) //nolint:gosec // an axle count cannot overflow int16
	return &v
}
