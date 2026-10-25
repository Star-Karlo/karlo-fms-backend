package vehicle

import (
	"time"

	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is the JSON representation of a vehicle, including a few joined labels.
type DTO struct {
	// Owning account — populated only in the platform-admin cross-account list.
	TenantID   *int64  `json:"tenant_id,omitempty"`
	TenantName *string `json:"tenant_name,omitempty"`
	ID         int64   `json:"id"`
	// MasterDataID pairs the row with karlo_masterdata (the id TMS uses).
	// Null until vehiclesync or write-through has paired it.
	MasterDataID   *string  `json:"master_data_id"`
	LicensePlate   string   `json:"license_plate"`
	Manufacturer   *string  `json:"manufacturer"`
	VehicleType    *string  `json:"vehicle_type"`
	BoxType        *string  `json:"box_type"`
	UnitYear       *int32   `json:"unit_year"`
	HullNo         *string  `json:"hull_no"`
	Color          *string  `json:"color"`
	FleetGroupID   *int64   `json:"fleet_group_id"`
	FleetGroupName *string  `json:"fleet_group_name"`
	Status         string   `json:"status"`
	Garage         *string  `json:"garage"`
	Odometer       *float64 `json:"odometer"`
	Hourmeter      *float64 `json:"hourmeter"`
	FuelTankLiters *float64 `json:"fuel_tank_liters"`
	FuelRatioKmpl  *float64 `json:"fuel_ratio_kmpl"`
	// Toll class and lorry dimensions.
	//
	// Axles decides the toll class — 2 is Golongan II, 3 is III, 4 is IV, 5 or
	// more is V — and nil means UNKNOWN, never a default. A guessed class
	// misprices every truck that has a real axle count, and does it invisibly
	// because the figure looks precise.
	//
	// GolonganOverride exists because two axles is ambiguous: BPJT splits
	// two-axle vehicles between Golongan I ("pick up / truk kecil") and
	// Golongan II on size, which an axle count cannot decide.
	//
	// The dimensions let routing apply the height, width and weight limits it
	// already reads from OpenStreetMap; without them every route uses
	// fleet-wide defaults.
	Axles            *int16     `json:"axles"`
	GolonganOverride *int16     `json:"golongan_override"`
	LengthM          *float64   `json:"length_m"`
	WidthM           *float64   `json:"width_m"`
	HeightM          *float64   `json:"height_m"`
	MaxWeightKg      *float64   `json:"max_weight_kg"`
	TrackerID        *int64     `json:"tracker_id"`
	TrackerImei      *string    `json:"tracker_imei"`
	TrackerPhone     *string    `json:"tracker_phone"`
	CurrentDriverID  *int64     `json:"current_driver_id"`
	DriverName       *string    `json:"driver_name"`
	DriverPhone      *string    `json:"driver_phone"`
	Notes            *string    `json:"notes"`
	CreatedAt        *time.Time `json:"created_at"`
	UpdatedAt        *time.Time `json:"updated_at"`
}

// CreateRequest is the create-vehicle body.
type CreateRequest struct {
	LicensePlate     string   `json:"license_plate"`
	Manufacturer     *string  `json:"manufacturer"`
	VehicleType      *string  `json:"vehicle_type"`
	BoxType          *string  `json:"box_type"`
	UnitYear         *int32   `json:"unit_year"`
	HullNo           *string  `json:"hull_no"`
	FleetGroupID     *int64   `json:"fleet_group_id"`
	Status           *string  `json:"status"`
	Garage           *string  `json:"garage"`
	Odometer         *float64 `json:"odometer"`
	Hourmeter        *float64 `json:"hourmeter"`
	FuelTankLiters   *float64 `json:"fuel_tank_liters"`
	FuelRatioKmpl    *float64 `json:"fuel_ratio_kmpl"`
	Axles            *int16   `json:"axles"`
	GolonganOverride *int16   `json:"golongan_override"`
	LengthM          *float64 `json:"length_m"`
	WidthM           *float64 `json:"width_m"`
	HeightM          *float64 `json:"height_m"`
	MaxWeightKg      *float64 `json:"max_weight_kg"`
	TrackerID        *int64   `json:"tracker_id"`
	CurrentDriverID  *int64   `json:"current_driver_id"`
	Notes            *string  `json:"notes"`
}

// UpdateRequest is the update-vehicle body (all fields optional/patch-style).
type UpdateRequest struct {
	LicensePlate     *string  `json:"license_plate"`
	Manufacturer     *string  `json:"manufacturer"`
	VehicleType      *string  `json:"vehicle_type"`
	BoxType          *string  `json:"box_type"`
	UnitYear         *int32   `json:"unit_year"`
	HullNo           *string  `json:"hull_no"`
	FleetGroupID     *int64   `json:"fleet_group_id"`
	Status           *string  `json:"status"`
	Garage           *string  `json:"garage"`
	Odometer         *float64 `json:"odometer"`
	Hourmeter        *float64 `json:"hourmeter"`
	FuelTankLiters   *float64 `json:"fuel_tank_liters"`
	FuelRatioKmpl    *float64 `json:"fuel_ratio_kmpl"`
	Axles            *int16   `json:"axles"`
	GolonganOverride *int16   `json:"golongan_override"`
	LengthM          *float64 `json:"length_m"`
	WidthM           *float64 `json:"width_m"`
	HeightM          *float64 `json:"height_m"`
	MaxWeightKg      *float64 `json:"max_weight_kg"`
	Notes            *string  `json:"notes"`
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
		ID: v.ID, MasterDataID: v.MasterDataID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		Axles: v.Axles, GolonganOverride: v.GolonganOverride,
		LengthM: v.LengthM, WidthM: v.WidthM, HeightM: v.HeightM,
		MaxWeightKg: v.MaxWeightKg,
		TrackerID:   v.TrackerID, CurrentDriverID: v.CurrentDriverID, Notes: v.Notes,
		CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}

func fromGetRow(v store.GetVehicleRow) DTO {
	return DTO{
		ID: v.ID, MasterDataID: v.MasterDataID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, FleetGroupName: v.FleetGroupName, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		Axles: v.Axles, GolonganOverride: v.GolonganOverride,
		LengthM: v.LengthM, WidthM: v.WidthM, HeightM: v.HeightM,
		MaxWeightKg: v.MaxWeightKg,
		TrackerID:   v.TrackerID, TrackerImei: v.TrackerImei, TrackerPhone: v.TrackerPhone,
		CurrentDriverID: v.CurrentDriverID, DriverName: v.DriverName, DriverPhone: v.DriverPhone,
		Notes: v.Notes, CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}

func fromListRow(v store.ListVehiclesRow) DTO {
	return DTO{
		ID: v.ID, MasterDataID: v.MasterDataID, LicensePlate: v.LicensePlate, Manufacturer: v.Manufacturer,
		VehicleType: v.VehicleType, BoxType: v.BoxType, UnitYear: v.UnitYear, HullNo: v.HullNo, Color: v.Color,
		FleetGroupID: v.FleetGroupID, FleetGroupName: v.FleetGroupName, Status: v.Status, Garage: v.Garage,
		Odometer: conv.NumF(v.Odometer), Hourmeter: conv.NumF(v.Hourmeter),
		FuelTankLiters: v.FuelTankLiters, FuelRatioKmpl: v.FuelRatioKmpl,
		Axles: v.Axles, GolonganOverride: v.GolonganOverride,
		LengthM: v.LengthM, WidthM: v.WidthM, HeightM: v.HeightM,
		MaxWeightKg: v.MaxWeightKg,
		TrackerID:   v.TrackerID, TrackerImei: v.TrackerImei,
		CurrentDriverID: v.CurrentDriverID, DriverName: v.DriverName, Notes: v.Notes,
		CreatedAt: conv.Time(v.CreatedAt), UpdatedAt: conv.Time(v.UpdatedAt),
	}
}
