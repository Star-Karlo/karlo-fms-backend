// Package mdwrite writes to master data AS THE SIGNED-IN USER.
//
// The migration moved the fleet register to karlo_masterdata and moved FMS's
// READS with it — vehiclesync projects master data into fms_app so the sixteen
// local joins keep working. The WRITES were never moved. So a vehicle created
// in FMS landed in fms_app and nowhere else: invisible to TMS, unpaired, and
// destined to be adopted-by-plate and partially overwritten if the same plate
// ever appeared upstream. A driver created in FMS landed in master data and
// nowhere else, with no employees row and therefore no bigint, which is why it
// could not be attached to a vehicle at all.
//
// This package closes that: every FMS write reaches master data first, and the
// local row is created already carrying the pairing.
//
// WHY THE CALLER'S TOKEN, NOT A SERVICE TOKEN.
//
// Every call here forwards the bearer of the person who made the request, plus
// their X-Acting-For. mdclient (the gRPC projection) presents a service token
// because it runs on a timer with nobody behind it; this runs inside a request
// that has a user, so it uses them.
//
// A service token would have been easier and is the wrong answer. It would
// authorise fms-the-service rather than the person: an FMS user who does not
// hold vehicles.edit in master data would succeed, when the correct outcome is
// that they are refused. FMS would have become a way around master data's
// permission model — the sort of gap that is invisible until someone edits a
// record they were never allowed to touch. Forwarding the caller also means
// master data's audit trail names a person instead of "fms-backend".
//
// The consequence to keep in mind: a 403 from here is usually CORRECT, and must
// be surfaced to the user rather than swallowed and retried with more
// authority.
package mdwrite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to master data's HTTP API on behalf of a request's caller.
type Client struct {
	base string
	hc   *http.Client
}

// New returns nil when baseURL is empty, which disables write-through — the
// same "unset means this deployment has not cut over" convention the rest of
// the shared-service wiring uses. Callers must nil-check.
func New(baseURL string, timeout time.Duration) *Client {
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Client{base: strings.TrimRight(baseURL, "/"), hc: &http.Client{Timeout: timeout}}
}

// Caller is the identity a write is performed as: the bearer token from the
// incoming request and, when Karlo staff are acting for a customer, the company
// they are acting for.
type Caller struct {
	Bearer string
	// ActingFor MUST be forwarded when present. Dropping it does not fail — it
	// writes the record to the STAFF MEMBER'S own company instead of the
	// customer's, silently, and the operator sees a success.
	ActingFor string
}

// CallerFrom extracts the identity to forward from an incoming request.
//
// Prefer CallerFor. This reads only the X-Acting-For HEADER, and FMS's own
// screens do not send one — they scope a staff member's action with
// ?account_id=N instead. See CallerFor for why that matters.
func CallerFrom(r *http.Request) Caller {
	return Caller{
		Bearer:    r.Header.Get("Authorization"),
		ActingFor: r.Header.Get("X-Acting-For"),
	}
}

// CompanyResolver maps a local tenant id to its master-data company.
type CompanyResolver interface {
	CompanyForTenant(ctx context.Context, tenantID int64) (string, bool)
}

// ErrUnresolvedActingFor means a staff member is acting for a tenant whose
// company could not be determined, so the upstream write would be misfiled.
var ErrUnresolvedActingFor = errors.New("acting for a tenant with no known master-data company; refusing to write upstream under the wrong company")

// CallerFor builds the identity to forward, TRANSLATING ?account_id INTO
// X-Acting-For when a staff member is acting for another customer.
//
// The two systems scope a staff action differently. FMS uses ?account_id=N,
// a local bigint, and its handlers resolve that through EffectiveTenant. Master
// data uses the X-Acting-For header carrying the company UUID. A write that
// forwards only what the request already carried therefore reaches master data
// with no acting-for at all whenever the operator came through FMS's own UI —
// and master data does the natural thing with a bare staff token: it files the
// record under the STAFF MEMBER'S OWN company. The vehicle shows up in FMS
// under Raja Besi and in TMS under Karlo Platform, and nothing reports the
// split.
//
// own is the caller's own tenant; effective is the one the request operates
// on. They differ only for staff using ?account_id. When they do, and no
// header was sent, the effective tenant's company is looked up and forwarded.
// If it cannot be resolved the write is REFUSED rather than sent: a missing
// acting-for does not fail upstream, it succeeds against the wrong company,
// which is the worse outcome by far.
func CallerFor(r *http.Request, own, effective int64, res CompanyResolver) (Caller, error) {
	c := CallerFrom(r)
	if c.ActingFor != "" || effective == own {
		return c, nil
	}
	if res == nil {
		return c, ErrUnresolvedActingFor
	}
	cid, ok := res.CompanyForTenant(r.Context(), effective)
	if !ok {
		return c, ErrUnresolvedActingFor
	}
	c.ActingFor = cid
	return c, nil
}

// Error is a non-2xx from master data, kept structured so handlers can pass the
// status through instead of flattening everything to a 500. A 403 here means
// the user genuinely may not do this upstream, and telling them so is the point.
type Error struct {
	Status int
	Body   string
	Op     string
}

func (e *Error) Error() string {
	return fmt.Sprintf("master data %s: %d %s", e.Op, e.Status, e.Body)
}

// envelope is master data's uniform response shape. Every list and single-object
// endpoint answers {"success":…, "data":…} — and it answers some refusals with a
// 2xx and success:false, so the flag is checked rather than the status alone.
type envelope struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (c *Client) do(ctx context.Context, caller Caller, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if caller.Bearer != "" {
		req.Header.Set("Authorization", caller.Bearer)
	}
	if caller.ActingFor != "" {
		req.Header.Set("X-Acting-For", caller.ActingFor)
	}

	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("master data %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))

	op := method + " " + path
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return &Error{Status: res.StatusCode, Body: string(raw), Op: op}
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		// Not JSON at all — almost always a misrouted base URL answering with a
		// document. Distinguishing that from an empty result matters: the same
		// mistake in the browser client rendered every shared-service screen as
		// empty with no error for a day.
		return &Error{Status: res.StatusCode, Body: "response was not JSON: " + snippet(raw), Op: op}
	}
	if !env.Success {
		return &Error{Status: res.StatusCode, Body: env.Message, Op: op}
	}
	if out != nil && len(env.Data) > 0 {
		return json.Unmarshal(env.Data, out)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}

// ---------------------------------------------------------------------------
// Vehicles
// ---------------------------------------------------------------------------

// Vehicle is the subset of master data's vehicle that FMS owns or needs back.
// Deliberately narrow: master data holds far more, and FMS must not send fields
// it does not own — odometer, fuel figures, tracker and driver links are FMS's
// own and have no place in a master-data write.
type Vehicle struct {
	ID            string `json:"id,omitempty"`
	LicensePlate  string `json:"licensePlate,omitempty"`
	ChassisNumber string `json:"chassisNumber,omitempty"`
	UnitType      string `json:"unitType,omitempty"`
	Year          int32  `json:"year,omitempty"`
	Status        string `json:"status,omitempty"`
	// Catalogue references, resolved from FMS's free-text names by
	// catalog.Resolver. Master data models a truck as head + body; there is
	// no truckTypeId on a vehicle.
	BrandID     string `json:"brandId,omitempty"`
	TruckBodyID string `json:"truckBodyId,omitempty"`
	TruckHeadID string `json:"truckHeadId,omitempty"`
	// Attributes MERGE upstream rather than replace, so writing fmsVehicleId
	// here does not erase whatever else the record carries.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// CreateVehicle returns the new master-data id, which the caller stores as the
// local row's master_data_id so it is born paired.
func (c *Client) CreateVehicle(ctx context.Context, caller Caller, v Vehicle) (string, error) {
	var out Vehicle
	if err := c.do(ctx, caller, http.MethodPost, "/api/v1/vehicles", v, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) UpdateVehicle(ctx context.Context, caller Caller, id string, v Vehicle) error {
	return c.do(ctx, caller, http.MethodPut, "/api/v1/vehicles/"+id, v, nil)
}

// SetVehicleDriver pairs (driverID = master-data driver id) or unpairs
// (nil) a vehicle's current driver upstream. Its own call rather than a
// Vehicle field: master data applies only the fields present, and on the
// wire "" is what unpairs — null decodes there as "leave unchanged".
func (c *Client) SetVehicleDriver(ctx context.Context, caller Caller, id string, driverID *string) error {
	v := ""
	if driverID != nil {
		v = *driverID
	}
	return c.do(ctx, caller, http.MethodPut, "/api/v1/vehicles/"+id, map[string]any{"currentDriverId": v}, nil)
}

func (c *Client) DeleteVehicle(ctx context.Context, caller Caller, id string) error {
	return c.do(ctx, caller, http.MethodDelete, "/api/v1/vehicles/"+id, nil, nil)
}

// ---------------------------------------------------------------------------
// Drivers
// ---------------------------------------------------------------------------

type Driver struct {
	ID            string         `json:"id,omitempty"`
	FullName      string         `json:"fullName,omitempty"`
	Phone         string         `json:"phone,omitempty"`
	EmployeeNo    string         `json:"employeeNo,omitempty"`
	LicenseNo     string         `json:"licenseNo,omitempty"`
	LicenseClass  string         `json:"licenseClass,omitempty"`
	LicenseExpiry string         `json:"licenseExpiry,omitempty"`
	Status        string         `json:"status,omitempty"`
	Attributes    map[string]any `json:"attributes,omitempty"`
}

func (c *Client) CreateDriver(ctx context.Context, caller Caller, d Driver) (string, error) {
	var out Driver
	if err := c.do(ctx, caller, http.MethodPost, "/api/v1/drivers", d, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) UpdateDriver(ctx context.Context, caller Caller, id string, d Driver) error {
	return c.do(ctx, caller, http.MethodPut, "/api/v1/drivers/"+id, d, nil)
}

func (c *Client) DeleteDriver(ctx context.Context, caller Caller, id string) error {
	return c.do(ctx, caller, http.MethodDelete, "/api/v1/drivers/"+id, nil, nil)
}

// StampDriverFMSID records the FMS bigint on the master-data driver, so the
// pairing resolves from either side. Attributes merge upstream, so this does
// not disturb anything else on the record.
func (c *Client) StampDriverFMSID(ctx context.Context, caller Caller, id string, fmsEmployeeID int64) error {
	return c.UpdateDriver(ctx, caller, id, Driver{
		Attributes: map[string]any{"fmsEmployeeId": fmsEmployeeID},
	})
}

// StampVehicleFMSID is the vehicle equivalent, used by the backfill so a row
// created in FMS before write-through existed can be matched from upstream too.
func (c *Client) StampVehicleFMSID(ctx context.Context, caller Caller, id string, fmsVehicleID int64) error {
	return c.UpdateVehicle(ctx, caller, id, Vehicle{
		Attributes: map[string]any{"fmsVehicleId": fmsVehicleID},
	})
}

// ---------------------------------------------------------------------------
// Trackers (GPS units and dashcams)
// ---------------------------------------------------------------------------

// Tracker is a device as master data holds it. Kind is "gps" (DeviceID the
// IMEI) or "dashcam" (DeviceID the VSS device id).
type Tracker struct {
	ID         string         `json:"id,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	DeviceID   string         `json:"deviceId,omitempty"`
	PhoneNo    string         `json:"phoneNo,omitempty"`
	Status     string         `json:"status,omitempty"` // active | inactive | retired
	Attributes map[string]any `json:"attributes,omitempty"`
}

// CreateTracker registers a device and returns its master-data id, which
// the caller stores as the local row's master_data_id.
func (c *Client) CreateTracker(ctx context.Context, caller Caller, t Tracker) (string, error) {
	var out Tracker
	if err := c.do(ctx, caller, http.MethodPost, "/api/v1/trackers", t, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) UpdateTracker(ctx context.Context, caller Caller, id string, t Tracker) error {
	return c.do(ctx, caller, http.MethodPut, "/api/v1/trackers/"+id, t, nil)
}

func (c *Client) DeleteTracker(ctx context.Context, caller Caller, id string) error {
	return c.do(ctx, caller, http.MethodDelete, "/api/v1/trackers/"+id, nil, nil)
}

// FitTracker records the device as fitted to a vehicle (master-data ids)
// from now; master data closes any previous fitting itself.
func (c *Client) FitTracker(ctx context.Context, caller Caller, id, vehicleID string) error {
	return c.do(ctx, caller, http.MethodPost, "/api/v1/trackers/"+id+"/fit", map[string]any{"vehicleId": vehicleID}, nil)
}

// UnfitTracker closes the device's current fitting.
func (c *Client) UnfitTracker(ctx context.Context, caller Caller, id string) error {
	return c.do(ctx, caller, http.MethodPost, "/api/v1/trackers/"+id+"/unfit", map[string]any{}, nil)
}

// ---------------------------------------------------------------------------
// Catalogues
// ---------------------------------------------------------------------------

// CatalogItem is a catalogue entry as master data returns it on create.
type CatalogItem struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	CompanyID string `json:"companyId,omitempty"`
}

// CreateCatalogItem adds an entry to a catalogue as the caller: a tenant's
// entry is company-scoped by master data, staff's is global. kind is the
// HTTP slug (brand | truckBody | truckHead); master data refuses tenant
// writes to Karlo-maintained catalogues (truckHead) with 403.
func (c *Client) CreateCatalogItem(ctx context.Context, caller Caller, kind, name string) (CatalogItem, error) {
	var out CatalogItem
	err := c.do(ctx, caller, http.MethodPost, "/api/v1/catalog/"+kind, map[string]any{"name": name}, &out)
	return out, err
}
