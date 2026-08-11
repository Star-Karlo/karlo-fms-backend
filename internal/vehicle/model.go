package vehicle

import (
	"time"

	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON representation of a vehicle, including a few joined labels.
type DTO struct {
	// Owning account — populated only in the platform-admin cross-account list.
	TenantID        *int64     `json:"tenant_id,omitempty"`
	TenantName      *string    `json:"tenant_name,omitempty"`
	ID              int64      `json:"id"`
	LicensePlate    string     `json:"license_plate"`
	Manufacturer    *string    `json:"manufacturer"`
	VehicleType     *string    `json:"vehicle_type"`
	BoxType         *string    `json:"box_type"`
	UnitYear        *int32     `json:"unit_year"`
	HullNo          *string    `json:"hull_no"`
	Color           *string    `json:"color"`
	FleetGroupID    *int64     `json:"fleet_group_id"`
	FleetGroupName  *string    `json:"fleet_group_name"`
	Status          string     `json:"status"`
	Garage          *string    `json:"garage"`
	Odometer        *float64   `json:"odometer"`
	Hourmeter       *float64   `json:"hourmeter"`
	FuelTankLiters  *float64   `json:"fuel_tank_liters"`
	FuelRatioKmpl   *float64   `json:"fuel_ratio_kmpl"`
	TrackerID       *int64     `json:"tracker_id"`
	TrackerImei     *string    `json:"tracker_imei"`
	TrackerPhone    *string    `json:"tracker_phone"`
	CurrentDriverID *int64     `json:"current_driver_id"`
	DriverName      *string    `json:"driver_name"`
	DriverPhone     *string    `json:"driver_phone"`
	Notes           *string    `json:"notes"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

// CreateRequest is the create-vehicle body.
type CreateRequest struct {
	LicensePlate    string   `json:"license_plate"`
	Manufacturer    *string  `json:"manufacturer"`
	VehicleType     *string  `json:"vehicle_type"`
	BoxType         *string  `json:"box_type"`
	UnitYear        *int32   `json:"unit_year"`
	HullNo          *string  `json:"hull_no"`
	FleetGroupID    *int64   `json:"fleet_group_id"`
	Status          *string  `json:"status"`
	Garage          *string  `json:"garage"`
	Odometer        *float64 `json:"odometer"`
	Hourmeter       *float64 `json:"hourmeter"`
	FuelTankLiters  *float64 `json:"fuel_tank_liters"`
	FuelRatioKmpl   *float64 `json:"fuel_ratio_kmpl"`
	TrackerID       *int64   `json:"tracker_id"`
	CurrentDriverID *int64   `json:"current_driver_id"`
	Notes           *string  `json:"notes"`
}

// UpdateRequest is the update-vehicle body (all fields optional/patch-style).
type UpdateRequest struct {
	LicensePlate   *string  `json:"license_plate"`
	Manufacturer   *string  `json:"manufacturer"`
	VehicleType    *string  `json:"vehicle_type"`
	BoxType        *string  `json:"box_type"`
	UnitYear       *int32   `json:"unit_year"`
	HullNo         *string  `json:"hull_no"`
	FleetGroupID   *int64   `json:"fleet_group_id"`
	Status         *string  `json:"status"`
	Garage         *string  `json:"garage"`
	Odometer       *float64 `json:"odometer"`
	Hourmeter      *float64 `json:"hourmeter"`
	FuelTankLiters *float64 `json:"fuel_tank_liters"`
	FuelRatioKmpl  *float64 `json:"fuel_ratio_kmpl"`
	Notes          *string  `json:"notes"`
}

// AssignTrackerRequest binds a tracker to a vehicle (null unbinds).
type AssignTrackerRequest struct {
	TrackerID *int64 `json:"tracker_id"`
}

// AssignDriverRequest sets a vehicle's current driver (null clears).
type AssignDriverRequest struct {
	DriverID *int64 `json:"driver_id"`
}

func fromVehicle(v store.Vehicle) DTO {
	return DTO{
		ID: v.ID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		TrackerID: v.TrackerID, CurrentDriverID: v.CurrentDriverID, Notes: v.Notes,
		CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}

func fromGetRow(v store.GetVehicleRow) DTO {
	return DTO{
		ID: v.ID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, FleetGroupName: v.FleetGroupName, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		TrackerID: v.TrackerID, TrackerImei: v.TrackerImei, TrackerPhone: v.TrackerPhone,
		CurrentDriverID: v.CurrentDriverID, DriverName: v.DriverName, DriverPhone: v.DriverPhone,
		Notes: v.Notes, CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}

func fromListRow(v store.ListVehiclesRow) DTO {
	return DTO{
		ID: v.ID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, FleetGroupName: v.FleetGroupName, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		TrackerID: v.TrackerID, TrackerImei: v.TrackerImei,
		CurrentDriverID: v.CurrentDriverID, DriverName: v.DriverName, Notes: v.Notes,
		CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}
