package liveview

import (
	"encoding/json"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// driveState buckets derived from a live fix; drives map marker colors.
const (
	StateMoving  = "moving"
	StateIdle    = "idle"
	StateParking = "parking"
	StateOffline = "offline"
)

// freshWindow is how recent a fix must be for a vehicle to count as online.
const freshWindow = 10 * time.Minute

// movingSpeed (km/h) above which a vehicle with a fix is considered moving.
const movingSpeed = 3.0

// Driver is the minimal driver summary embedded in a live vehicle.
type Driver struct {
	ID    *int64  `json:"id"`
	Name  *string `json:"name"`
	Phone *string `json:"phone"`
	// MasterDataID is the shared-platform driver id — the identity the K-Trip
	// phone app reports positions under. Empty when the driver is unpaired.
	MasterDataID string `json:"master_data_id,omitempty"`
}

// Tracker is the minimal tracker summary embedded in a live vehicle.
type Tracker struct {
	ID   int64  `json:"id"`
	IMEI string `json:"imei"`
}

// Position is the JSON-friendly live fix.
type Position struct {
	// Source is where the fix came from, in order of preference: "tracker"
	// (GPS unit), "camera" (the dashcam's own GPS, for vehicles without a
	// tracker), or "phone" (the assigned driver's K-Trip app, when neither
	// device has a fix). A phone follows the person, not the truck: it
	// locates the vehicle only while that driver is assigned to it.
	Source    string    `json:"source,omitempty"`
	Lat       *float64  `json:"lat"`
	Lon       *float64  `json:"lon"`
	Speed     *float64  `json:"speed"`
	Bearing   *float64  `json:"bearing"`
	Ignition  *bool     `json:"ignition"`
	Time      time.Time `json:"time"`
	Kecamatan string    `json:"kecamatan"`
	Kabupaten string    `json:"kabupaten"`
	Kota      string    `json:"kota"`
	Provinsi  string    `json:"provinsi"`
	Jalan     string    `json:"jalan"`
	// Address is the most specific place name available, ready to display —
	// street when there is one, otherwise district and city.
	//
	// A third of the fleet's fixes carry no street: yards, ports and
	// plantation roads have no name, and Semarang alone accounts for 250 of
	// them at any moment. A consumer that shows `jalan` and falls back to
	// "unknown" therefore labels ~600 trucks unknown while knowing their
	// district and city perfectly well — which is what TMS's Control Tower
	// did. Composing it here means every consumer gets the same answer and
	// none has to know the five fields' precedence.
	Address string `json:"address"`
	// Metadata is the raw per-device sensor blob (fuel, voltages, GSM/GNSS, etc.).
	// Only populated on the single-vehicle detail path; the bulk list trims it.
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// VehicleLive is a registry vehicle enriched with its live position + state.
type VehicleLive struct {
	// Owning account — populated ONLY in the platform-admin cross-tenant list, so
	// the operator (and the detail panel) knows which account a vehicle is in.
	TenantID   *int64  `json:"tenant_id,omitempty"`
	TenantName *string `json:"tenant_name,omitempty"`
	VehicleID  int64   `json:"vehicle_id"`
	// MasterDataID is the vehicle's id in karlo_masterdata — the identity the
	// rest of the platform keys on. VehicleID above is FMS-local and means
	// nothing to TMS, so a caller correlating a truck across products joins on
	// this. NULL until vehiclesync has paired the row; the alternative for
	// callers is matching on the plate, which is not a stable identity (see
	// migration 0042 — this dataset contains six re-plated trucks).
	MasterDataID   *string   `json:"master_data_id"`
	LicensePlate   string    `json:"license_plate"`
	VehicleType    *string   `json:"vehicle_type"`
	RegistryStatus string    `json:"registry_status"`
	FleetGroupName *string   `json:"fleet_group_name"`
	Driver         Driver    `json:"driver"`
	Tracker        Tracker   `json:"tracker"`
	Position       *Position `json:"position"`
	// CameraDeviceID is the linked dashcam's VSS id, when the vehicle has one.
	// Present whether or not the camera is the position source.
	CameraDeviceID string   `json:"camera_device_id,omitempty"`
	Online         bool     `json:"online"`
	DriveState     string   `json:"drive_state"`
	StaleMinutes   *float64 `json:"stale_minutes"`
	// StateSince is when the current DriveState began, so a list can say
	// "idle for 40 min" instead of a speed. Moving/idle/parking come from the
	// telemetry current-state row, which keeps the timestamp of the last
	// class change; for offline it is the last fix (the truck has not been
	// heard from since). Omitted for camera-only positions and for rows
	// written before the telemetry service kept it.
	StateSince *time.Time `json:"state_since,omitempty"`
	// From the telemetry collector's state table (updated ~2min) — cheap to
	// join fleet-wide, powers list filters without shipping metadata blobs.
	BatteryV  *float64 `json:"battery_v"`
	GsmSignal *float64 `json:"gsm_signal"`
	// TankLitres is the vehicle's fuel tank size, from the FMS vehicle
	// record (master data does not carry it). Nil when nobody has set it.
	TankLitres *float64 `json:"tank_litres"`
	// Maintenance context: km driven since the last recorded service. Nil when
	// unknown; HasServiceRecord=false means no maintenance record exists yet.
	KmSinceService   *float64 `json:"km_since_service"`
	HasServiceRecord bool     `json:"has_service_record"`
}

// enrich merges a telemetry fix into the vehicle, computing online + drive state.
func enrich(v *VehicleLive, p *telemetry.Position, now time.Time) {
	if p == nil || p.Lat == nil || p.Lon == nil {
		v.Online = false
		v.DriveState = StateOffline
		return
	}
	pos := &Position{
		Source: "tracker",
		Lat:    p.Lat, Lon: p.Lon, Speed: p.Speed, Bearing: p.Bearing, Ignition: p.Ignition,
		Time: p.Time, Kecamatan: p.Kecamatan, Kabupaten: p.Kabupaten, Kota: p.Kota,
		Provinsi: p.Provinsi, Jalan: p.Jalan, Metadata: p.Metadata,
		Address: composeAddress(p.Jalan, p.Kecamatan, p.Kota, p.Kabupaten, p.Provinsi),
	}
	v.Position = pos
	stale := now.Sub(p.Time).Minutes()
	v.StaleMinutes = &stale
	if now.Sub(p.Time) > freshWindow {
		v.Online = false
		v.DriveState = StateOffline
		t := p.Time
		v.StateSince = &t
		return
	}
	v.Online = true
	v.StateSince = p.StateSince
	switch {
	case p.Speed != nil && *p.Speed > movingSpeed:
		v.DriveState = StateMoving
	case p.Ignition != nil && *p.Ignition:
		v.DriveState = StateIdle
	default:
		v.DriveState = StateParking
	}
}

// enrichCamera places a vehicle by its dashcam's own GPS — the fallback for
// vehicles with no tracker. The camera reports a last-known position, not a live
// motion stream, so there is no speed/ignition to derive drive state from: an
// online device with a fix is shown as located (parking), otherwise offline.
func enrichCamera(v *VehicleLive, lat, lon *float64, online bool, now time.Time) {
	if lat == nil || lon == nil {
		return // keep whatever the tracker path left (offline)
	}
	v.Position = &Position{Source: "camera", Lat: lat, Lon: lon, Time: now}
	v.Online = online
	if online {
		v.DriveState = StateParking
	} else {
		v.DriveState = StateOffline
	}
}

// enrichPhone places a vehicle by its assigned driver's phone — the last
// resort after tracker and camera. The phone has speed but no ignition, so
// the state is moving or parking, never idle; offline is the same age rule
// as the tracker's, and state_since is the last fix in that case (the phone
// keeps no state stamp).
func enrichPhone(v *VehicleLive, p *telemetry.MobilePosition, now time.Time) {
	if p == nil || p.Lat == nil || p.Lon == nil {
		return // keep whatever the earlier sources left (offline)
	}
	v.Position = &Position{
		Source: "phone",
		Lat:    p.Lat, Lon: p.Lon, Speed: p.Speed, Bearing: p.Bearing,
		Time: p.Time, Kecamatan: p.Kecamatan, Kabupaten: p.Kabupaten, Kota: p.Kota,
		Provinsi: p.Provinsi, Jalan: p.Jalan,
		Address: composeAddress(p.Jalan, p.Kecamatan, p.Kota, p.Kabupaten, p.Provinsi),
	}
	stale := now.Sub(p.Time).Minutes()
	v.StaleMinutes = &stale
	if now.Sub(p.Time) > freshWindow {
		v.Online = false
		v.DriveState = StateOffline
		t := p.Time
		v.StateSince = &t
		return
	}
	v.Online = true
	if p.Speed != nil && *p.Speed > movingSpeed {
		v.DriveState = StateMoving
	} else {
		v.DriveState = StateParking
	}
}

// composeAddress joins the place parts that exist, most specific first. Kota
// and kabupaten are alternatives (city vs regency), so only the one present
// is used; the result is never empty when any part is known.
func composeAddress(jalan, kecamatan, kota, kabupaten, provinsi string) string {
	city := kota
	if city == "" {
		city = kabupaten
	}
	out := ""
	for _, part := range []string{jalan, kecamatan, city, provinsi} {
		if part == "" {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += part
	}
	return out
}
