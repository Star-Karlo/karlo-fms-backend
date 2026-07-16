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
