//go:build integration

package liveview

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/dbtest"
)

// truck inserts a vehicle, with a tracker unless tracked is false.
func truck(t *testing.T, pool *db.Pool, tid int64, n int, status string, tracked bool) int64 {
	t.Helper()
	var tracker *int64
	if tracked {
		id := dbtest.QueryID(t, pool, tid,
			`INSERT INTO trackers (tenant_id, imei) VALUES ($1, $2) RETURNING id`, tid, fmt.Sprintf("TEST-%d-%d", tid, n))
		tracker = &id
	}
	return dbtest.QueryID(t, pool, tid,
		`INSERT INTO vehicles (tenant_id, license_plate, status, tracker_id) VALUES ($1, $2, $3, $4) RETURNING id`,
		tid, fmt.Sprintf("B %d %d T", tid, n), status, tracker)
}

func TestTrackedVehicleIDsPageIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := dbtest.Tenant(t, pool, "bulk A")
	b := dbtest.Tenant(t, pool, "bulk B")
	// dbtest.Tenant removes vehicles but not trackers; this runs first.
	t.Cleanup(func() {
		for _, tid := range []int64{a, b} {
			dbtest.Exec(t, pool, tid, `DELETE FROM vehicles`)
			dbtest.Exec(t, pool, tid, `DELETE FROM trackers`)
		}
	})

	want := []int64{
		truck(t, pool, a, 1, "active", true),
		truck(t, pool, a, 2, "repair", true),
		truck(t, pool, a, 3, "active", true),
	}
	truck(t, pool, a, 4, "archive", true) // left the fleet
	truck(t, pool, a, 5, "active", false) // nothing to read history from
	truck(t, pool, b, 1, "active", true)  // another account

	svc := NewService(pool, nil)
	ctx := context.Background()

	all, err := svc.trackedVehicleIDs(ctx, a, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(all, want) {
		t.Fatalf("all = %v, want %v (tracked, in the fleet, this account, id order)", all, want)
	}

	first, _ := svc.trackedVehicleIDs(ctx, a, 0, 2)
	cursor := nextCursor(first, 2)
	rest, _ := svc.trackedVehicleIDs(ctx, a, first[len(first)-1], 2)
	if !slices.Equal(first, want[:2]) || cursor == "" || !slices.Equal(rest, want[2:]) || nextCursor(rest, 2) != "" {
		t.Errorf("pages = %v (cursor %q) then %v, want %v then %v and no further cursor", first, cursor, rest, want[:2], want[2:])
	}
}
