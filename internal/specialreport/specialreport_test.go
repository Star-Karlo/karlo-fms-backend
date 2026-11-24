package specialreport

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/telemetry"
)

// The expected values below are copied from the legacy feed this report
// replaces (response captured 2026-11-24), so a pass means FMS answers what
// that feed answered for the same inputs.

func f(v float64) *float64 { return &v }
func ts(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t
}

func gpsFix(lat, lon, speed float64, ign bool, mv float64, at string) *telemetry.Position {
	t := ts(at)
	meta, _ := json.Marshal(map[string]any{"External Voltage": mv, "altitude": 49.0})
	return &telemetry.Position{Lat: f(lat), Lon: f(lon), Speed: f(speed), Bearing: f(280),
		Ignition: &ign, Time: t, ReceivedAt: &t, Metadata: meta}
}

func phoneFix(lat, lon float64, at string) *telemetry.MobilePosition {
	meta, _ := json.Marshal(map[string]any{"batteryLevel": 27.0, "gpsOn": true, "altitude": 91.2})
	return &telemetry.MobilePosition{Lat: f(lat), Lon: f(lon), Speed: f(0), Bearing: f(0), Time: ts(at), Metadata: meta}
}

func TestDistanceMatchesLegacy(t *testing.T) {
	cases := []struct {
		plate                  string
		tLat, tLon, dLat, dLon float64
		want                   float64
	}{
		{"H-9070-OH", -6.3676866, 107.3623933, -6.4258087, 107.4801472, 14528.752434445016},
		{"H-9361-OH", -6.7813, 110.995033333333, -6.781296, 110.9950137, 2.2130146588909305},
		{"H-9471-OH", -6.407385, 107.2265733, -6.4070368, 107.2240962, 276.4456932833777},
	}
	for _, c := range cases {
		row := buildRow(truck{plate: c.plate, imei: "1", driverID: "d"},
			gpsFix(c.tLat, c.tLon, 0, false, 25575, "2026-11-24T02:17:24.264Z"),
			phoneFix(c.dLat, c.dLon, "2026-11-21T22:28:07.241Z"))
		if row.DistanceDriverToTruck == nil {
			t.Fatalf("%s: distance is null", c.plate)
		}
		// Legacy reported to full float precision; agree to the millimetre.
		if math.Abs(*row.DistanceDriverToTruck-c.want) > 0.001 {
			t.Errorf("%s: distance %v, legacy %v", c.plate, *row.DistanceDriverToTruck, c.want)
		}
	}
}

func TestBatteryFlags(t *testing.T) {
	cases := []struct {
		mv   float64
		want string
	}{
		{25575, "NORMAL"},       // H-9070-OH 25.575
		{24507, "NORMAL"},       // H-9384-OH 24.507
		{24483, "BATT_WARNING"}, // H-9412-OF 24.483
		{8240, "BATT_WARNING"},  // B-9093-PFX 8.24
		{7863, "BATT_LOW"},      // H-8497-OF 7.863
		{0, "BATT_LOW"},         // H-9927-OA style 0 V
	}
	for _, c := range cases {
		loc := truckFromTracker(gpsFix(-6.9, 110.4, 0, false, c.mv, "2026-11-24T02:00:00Z"))
		if len(loc.ErrorDesc) != 1 || loc.ErrorDesc[0] != c.want {
			t.Errorf("%v mV: errorDesc %v, want [%s]", c.mv, loc.ErrorDesc, c.want)
		}
		if got := *loc.BatteryLevel; math.Abs(got-c.mv/1000) > 1e-9 {
			t.Errorf("%v mV: batteryLevel %v", c.mv, got)
		}
	}
}

func TestLostGPS(t *testing.T) {
	// H-1920-EF: 0,0 fix at 0 V -> ["BATT_LOW","LOST_GPS"].
	loc := truckFromTracker(gpsFix(0, 0, 0, false, 0, "2026-02-02T00:35:00.773Z"))
	if strings.Join(loc.ErrorDesc, ",") != "BATT_LOW,LOST_GPS" {
		t.Errorf("errorDesc %v", loc.ErrorDesc)
	}
}

func TestStatus(t *testing.T) {
	if s := status(f(1), nil); s != "Moving" { // H-1440-DF: speed 1 -> Moving
		t.Errorf("speed 1: %s", s)
	}
	on := true
	if s := status(f(0), &on); s != "Idle" {
		t.Errorf("ignition on at rest: %s", s)
	}
	if s := status(f(0), nil); s != "Stop" {
		t.Errorf("at rest: %s", s)
	}
}

func TestPhoneStandsInWhenNewer(t *testing.T) {
	// H-1450-FA: tracker last fixed 2026-11-17, phone 2026-11-23 -> the phone
	// is the truck's location, distance 0, lastGPS still the tracker's time.
	gps := gpsFix(-6.97, 109.96, 0, false, 25000, "2026-11-17T14:54:42.976Z")
	row := buildRow(truck{plate: "H-1450-FA", imei: "1", driverID: "69048a937d92a31fe7b97a7c"},
		gps, phoneFix(-6.968513, 109.963313, "2026-11-23T11:55:41.316Z"))

	if row.LastLocation.Latitude != -6.968513 || row.LastLocation.Status != "" {
		t.Errorf("lastLocation not the phone fix: %+v", row.LastLocation)
	}
	if row.LastLocation.LastGPS == nil || !row.LastLocation.LastGPS.Equal(gps.Time) {
		t.Errorf("lastGPS %v, want tracker time %v", row.LastLocation.LastGPS, gps.Time)
	}
	if *row.DistanceDriverToTruck != 0 {
		t.Errorf("distance %v, want 0", *row.DistanceDriverToTruck)
	}
}

func TestTrackerKeptWhenNewer(t *testing.T) {
	// H-9065-OH: tracker 2026-11-17, phone 2026-11-11 -> tracker kept.
	row := buildRow(truck{plate: "H-9065-OH", imei: "1", driverID: "d"},
		gpsFix(-7.43106, 112.4804283, 0, false, 0, "2026-11-17T07:10:01.316Z"),
		phoneFix(-7.6077901, 112.5411533, "2026-11-11T09:54:56.261Z"))
	if row.LastLocation.Status != "Stop" || row.LastLocation.Latitude != -7.43106 {
		t.Errorf("tracker fix replaced: %+v", row.LastLocation)
	}
	if math.Abs(*row.DistanceDriverToTruck-20760.42) > 1 {
		t.Errorf("distance %v, legacy 20760.42", *row.DistanceDriverToTruck)
	}
}

func TestNoDataShape(t *testing.T) {
	// H-1846-EF: no tracker fix, no driver -> lastLocation null, no driver key.
	id := "601a000d28e7a4430e73fa66"
	b, _ := json.Marshal(buildRow(truck{masterDataID: &id, plate: "H-1846-EF"}, nil, nil))
	want := `{"_id":"601a000d28e7a4430e73fa66","policeNumber":"H-1846-EF","lastLocation":null,"driverLastLocation":null,"distanceDriverToTruck":null}`
	if string(b) != want {
		t.Errorf("got  %s\nwant %s", b, want)
	}
}
