package liveview

import (
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
}

// Tracker is the minimal tracker summary embedded in a live vehicle.
type Tracker struct {
	ID   int64  `json:"id"`
	IMEI string `json:"imei"`
}

// Position is the JSON-friendly live fix.
type Position struct {
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
}

// VehicleLive is a registry vehicle enriched with its live position + state.
type VehicleLive struct {
	VehicleID      int64     `json:"vehicle_id"`
	LicensePlate   string    `json:"license_plate"`
	RegistryStatus string    `json:"registry_status"`
	FleetGroupName *string   `json:"fleet_group_name"`
	Driver         Driver    `json:"driver"`
	Tracker        Tracker   `json:"tracker"`
	Position       *Position `json:"position"`
	Online         bool      `json:"online"`
	DriveState     string    `json:"drive_state"`
	StaleMinutes   *float64  `json:"stale_minutes"`
}

// enrich merges a telemetry fix into the vehicle, computing online + drive state.
func enrich(v *VehicleLive, p *telemetry.Position, now time.Time) {
	if p == nil || p.Lat == nil || p.Lon == nil {
		v.Online = false
		v.DriveState = StateOffline
		return
	}
	pos := &Position{
		Lat: p.Lat, Lon: p.Lon, Speed: p.Speed, Bearing: p.Bearing, Ignition: p.Ignition,
		Time: p.Time, Kecamatan: p.Kecamatan, Kabupaten: p.Kabupaten, Kota: p.Kota,
		Provinsi: p.Provinsi, Jalan: p.Jalan,
	}
	v.Position = pos
	stale := now.Sub(p.Time).Minutes()
	v.StaleMinutes = &stale
	if now.Sub(p.Time) > freshWindow {
		v.Online = false
		v.DriveState = StateOffline
		return
	}
	v.Online = true
	switch {
	case p.Speed != nil && *p.Speed > movingSpeed:
		v.DriveState = StateMoving
	case p.Ignition != nil && *p.Ignition:
		v.DriveState = StateIdle
	default:
		v.DriveState = StateParking
	}
}
