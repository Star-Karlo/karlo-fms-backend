// Package dbtest gives integration tests (build tag "integration") a real
// database connection as the app role, plus throwaway tenants.
//
// It connects as fms_app on purpose: the table owner bypasses RLS, so a test
// run as postgres would pass even with tenant isolation broken.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// Open connects to DATABASE_URL (default: the compose PostGIS on :5433 as
// fms_app) and fails the test if that role would bypass RLS. Sessions run in
// Asia/Jakarta, so SQL that silently depends on a UTC session fails here.
func Open(t *testing.T) *db.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = "postgres://fms_app:fms_app@localhost:5433/fms_app"
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatalf("dbtest: DATABASE_URL: %v", err)
	}
	cfg.ConnConfig.RuntimeParams["timezone"] = "Asia/Jakarta"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err == nil {
		err = pool.Ping(ctx)
	}
	if err != nil {
		t.Fatalf("dbtest: connect (run make up && make migrate): %v", err)
	}
	t.Cleanup(pool.Close)
	var bypass bool
	if err := pool.QueryRow(ctx,
		`SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user`).Scan(&bypass); err != nil || bypass {
		t.Fatalf("dbtest: connect as a role that is subject to RLS (fms_app), not the owner (err %v)", err)
	}
	return pool
}

// Tenant creates a tenant that is removed, with its vehicles, cameras and
// clips, when the test ends.
func Tenant(t *testing.T, pool *db.Pool, name string) int64 {
	t.Helper()
	ctx := context.Background()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	var id int64
	if err := pool.QueryRow(ctx, `INSERT INTO tenants (name, slug) VALUES ($1, $2) RETURNING id`,
		name, "dbtest-"+hex.EncodeToString(b)).Scan(&id); err != nil {
		t.Fatalf("dbtest: create tenant: %v", err)
	}
	t.Cleanup(func() {
		err := db.Tx(ctx, pool, id, func(tx pgx.Tx) error {
			for _, q := range []string{`DELETE FROM video_clips`, `DELETE FROM dashcams`, `DELETE FROM vehicles`} {
				if _, err := tx.Exec(ctx, q); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `DELETE FROM tenants WHERE id = $1`, id)
			return err
		})
		if err != nil {
			t.Errorf("dbtest: clean up tenant %d: %v", id, err)
		}
	})
	return id
}

// Exec runs one statement in the tenant's RLS scope.
func Exec(t *testing.T, pool *db.Pool, tenantID int64, sql string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		t.Fatalf("dbtest: %s: %v", sql, err)
	}
}

// QueryID runs an INSERT ... RETURNING id in the tenant's RLS scope.
func QueryID(t *testing.T, pool *db.Pool, tenantID int64, sql string, args ...any) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, args...).Scan(&id)
	}); err != nil {
		t.Fatalf("dbtest: %s: %v", sql, err)
	}
	return id
}
