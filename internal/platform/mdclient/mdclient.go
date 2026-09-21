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
