// Package tenantsync keeps fms_app.tenants as a PROJECTION of the companies
// held in the shared authentication service.
//
// WHY A PROJECTION RATHER THAN DELETING THE TABLE.
//
// Fourteen places in this service iterate tenants — the webhook dispatcher, the
// retention sweeper, three dashcam pollers, telemetry sync, driver scoring,
// custom report runs, and four building a dropdown. They do it in fourteen
// different shapes: some on the pool, some inside an open transaction, some
// scanning an id and some an id and a name. Rewriting all fourteen to call an
// HTTP client would put a network dependency inside transactions that currently
// cannot fail that way, in the least-tested paths in the service.
//
// So the SQL stays and the DATA moves: the authoritative record is
// karlo_auth.companies, and this keeps a local copy in step. The table is a
// cache with a refresher, not a second source of truth — nothing writes to it
// except this package.
//
// WHAT IT FIXES. After the cutover a company is created in the auth service and
// allocated an fms_tenant_id there, so a customer can exist with no row here at
// all. Row-level security still works — it only compares the bigint — but every
// one of those fourteen loops would silently skip them: no webhooks, no
// retention, no driver scores, and nothing anywhere explaining why. The
// refresher inserts the row before that can happen.
//
// WHAT IT DELIBERATELY DOES NOT DO: delete. A company removed upstream leaves
// its row here, because fms_app still holds that tenant's vehicles, alerts and
// telemetry, all foreign-keyed to it. Removing the row would orphan live data
// to tidy a cache. Retiring a customer's data is a decision with its own
// process, not a side effect of a sync loop.
package tenantsync

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/tenantdir"
)

// Syncer refreshes the local projection.
type Syncer struct {
	pool *db.Pool
	dir  *tenantdir.Directory
	wake chan struct{}
}

// Wake asks for a refresh now — a karlo:companies notice (a company
// created, renamed, or granted FMS). Never blocks; wakes coalesce.
func (s *Syncer) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func New(pool *db.Pool, dir *tenantdir.Directory) *Syncer {
	if dir == nil {
		return nil
	}
	return &Syncer{pool: pool, dir: dir, wake: make(chan struct{}, 1)}
}

// Run refreshes immediately, then on every tick.
//
// The first pass is synchronous-ish on purpose: a worker that starts before the
// projection is populated would do one sweep over a stale tenant list, and for
// the retention sweeper that is a sweep that skips a customer's expiring rows.
func (s *Syncer) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	if err := s.Once(ctx); err != nil {
		log.Printf("tenantsync: first refresh failed, workers will use the existing rows: %v", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
			// A company notice: the directory's minute-old copy is stale.
			s.dir.Invalidate()
		}
		if err := s.Once(ctx); err != nil {
			log.Printf("tenantsync: %v", err)
		}
	}
}

// Once brings the projection up to date.
func (s *Syncer) Once(ctx context.Context) error {
	companies, err := s.dir.List(ctx)
	if err != nil {
		return err
	}
	if len(companies) == 0 {
		// Refusing to act on an empty list rather than treating it as "no
		// customers". An auth service that answers 200 with nothing — a bad
		// deploy, an empty database, a filter gone wrong — would otherwise be
		// indistinguishable from every customer having been removed.
		log.Printf("tenantsync: the company list came back empty; leaving the projection alone")
		return nil
	}

	var inserted, renamed int
	err = db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		for _, c := range companies {
			// id is GENERATED ALWAYS, so the alias has to be written with
			// OVERRIDING SYSTEM VALUE: these ids are assigned upstream and this
			// table no longer allocates its own.
			ct, e := tx.Exec(ctx, `
				INSERT INTO tenants (id, name, slug)
				OVERRIDING SYSTEM VALUE
				VALUES ($1, $2, $3)
				ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name
				WHERE tenants.name IS DISTINCT FROM EXCLUDED.name`,
				c.ID, c.Name, slugFor(c))
			if e != nil {
				return e
			}
			if ct.RowsAffected() == 1 {
				// Cannot tell insert from update from the tag alone; counted
				// together and reported as "changed", which is all the log is
				// for.
				inserted++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if inserted > 0 {
		log.Printf("tenantsync: %d tenant row(s) added or renamed from the authentication service", inserted+renamed)
	}
	return nil
}

// slugFor is only used on INSERT — the column is NOT NULL and UNIQUE, and an
// existing row keeps whatever slug it already had. The auth service's slug is
// nullable and mostly unset, so the id is a stable fallback that cannot collide.
func slugFor(c tenantdir.Tenant) string {
	return "company-" + itoa(c.ID)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
