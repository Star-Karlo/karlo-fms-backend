package liveview

import (
	"slices"
	"testing"

	"github.com/karlo/fms-backend/internal/store"
)

// A hull number must name one truck or be refused: answering for one of two
// would return the wrong truck's figures with a 200.
func TestPickHull(t *testing.T) {
	v := func(id int64, plate, status string) store.ListVehiclesByHullRow {
		return store.ListVehiclesByHullRow{ID: id, LicensePlate: plate, Status: status}
	}
	cases := []struct {
		name      string
		rows      []store.ListVehiclesByHullRow
		wantID    int64
		wantTieOf []string
	}{
		{"nothing matched", nil, 0, nil},
		{"one active", []store.ListVehiclesByHullRow{v(1, "B 1 A", "active")}, 1, nil},
		{"one in repair", []store.ListVehiclesByHullRow{v(2, "B 2 A", "repair")}, 2, nil},
		{"only archived still answers, for history", []store.ListVehiclesByHullRow{v(3, "B 3 A", "archive")}, 3, nil},
		{"number reassigned: the fleet truck wins", []store.ListVehiclesByHullRow{v(4, "B 4 A", "archive"), v(5, "B 5 A", "active")}, 5, nil},
		{"a truck in repair is still in the fleet", []store.ListVehiclesByHullRow{v(6, "B 6 A", "archive"), v(7, "B 7 A", "repair")}, 7, nil},
		{"two in the fleet is a tie", []store.ListVehiclesByHullRow{v(8, "B 8 A", "active"), v(9, "B 9 A", "repair")}, 0, []string{"B 8 A", "B 9 A"}},
		{"two archived and none in the fleet is a tie", []store.ListVehiclesByHullRow{v(10, "B 10 A", "archive"), v(11, "B 11 A", "archive")}, 0, []string{"B 10 A", "B 11 A"}},
	}
	for _, c := range cases {
		id, tie := pickHull(c.rows)
		if id != c.wantID || !slices.Equal(tie, c.wantTieOf) {
			t.Errorf("%s: pickHull = (%d, %v), want (%d, %v)", c.name, id, tie, c.wantID, c.wantTieOf)
		}
	}
}
