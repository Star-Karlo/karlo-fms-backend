// Package db provides the pgx connection pool and the per-request,
// tenant-scoped transaction helper that drives Postgres Row-Level Security.
//
// Every tenant-scoped query must run inside Tx(): it opens a transaction, sets
// `app.current_tenant` with SET LOCAL (transaction-scoped, so it never leaks
// across pooled connections), runs the caller's function, and commits. This is
// the app-layer half of the RLS model defined in db/migrations/0005_rls.sql.
package db

import (
	"context"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the shared connection pool type alias (keeps import surface small).
type Pool = pgxpool.Pool

// New opens and pings a pgx pool.
func New(ctx context.Context, url string) (*Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// FleetProjectionLock is the advisory-lock class shared by the background jobs
// that bulk-write one tenant's `vehicles` rows. See LockFleetProjection.
const FleetProjectionLock = 42

// DashcamAutosaveLock is the advisory-lock class that keeps the dashcam
// auto-save to one backend instance at a time (session lock, key 0).
const DashcamAutosaveLock = 43

// LockFleetProjection serialises this transaction against the other background
// job that bulk-writes the same tenant's vehicles.
//
// vehiclesync and telemetrysync both take row locks across a tenant's whole
// fleet, in ORDERS THAT CANNOT BE RECONCILED: vehiclesync walks master data and
// matches rows by plate, so it never learns a vehicle's bigint id; telemetrysync
// walks the telemetry response, which arrives in device order. Two transactions
// taking the same rows in different orders is the textbook deadlock, and it is
// not hypothetical — it fired twice within three minutes of the cutover, on
// tenant 1 (~1,900 trucks), aborting a vehiclesync pass and a telemetry pass:
//
//	vehiclesync: Siba Surya (tenant 1): ERROR: deadlock detected (SQLSTATE 40P01)
//	telemetrysync: tenant 1: ERROR: deadlock detected (SQLSTATE 40P01)
//
// It went quiet on its own only because vehiclesync converged to zero writes
// once every row was paired; the next bulk change upstream would reopen it.
//
// A shared lock ORDER would be the tidier fix, but it would mean teaching
// vehiclesync to resolve plate -> id up front and sort by it, restructuring the
// job that had just been deployed. Serialising is a few lines and is provably
// sufficient: the two never hold each other's rows because they never run
// together on a tenant. Both are periodic (5m and 2m) and a whole-fleet pass is
// ~20s, so the wait costs a delayed tick, not a dropped one — and a delayed tick
// is what the deadlock produced anyway, minus the error and the lost work.
//
// Transaction-scoped, so it is released by COMMIT or ROLLBACK; there is no
// unlock path to leak.
func LockFleetProjection(ctx context.Context, tx pgx.Tx, tenantID int64) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1, $2)", int32(FleetProjectionLock), int32(tenantID))
	return err
}

// Tx runs fn inside a tenant-scoped transaction. tenantID is applied via
// SET LOCAL so RLS policies filter every statement to that tenant. If fn
// returns an error the transaction is rolled back.
func Tx(ctx context.Context, pool *Pool, tenantID int64, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after a successful commit

	// set_config(..., true) is transaction-local (like SET LOCAL) and safely
	// parameterized. The value is text; the RLS policies cast it to bigint.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_tenant', $1, true)", strconv.FormatInt(tenantID, 10)); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SystemTx runs fn inside a transaction WITHOUT a tenant scope. Used for the few
// pre-tenant operations (login user lookup) that must read across tenants.
// Callers are responsible for their own WHERE clauses.
func SystemTx(ctx context.Context, pool *Pool, fn func(tx pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// Deterministically clear any tenant scope inherited from a pooled
	// connection so login-bootstrap policies see an unset tenant.
	if _, err := tx.Exec(ctx, "SELECT set_config('app.current_tenant', '', true)"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
