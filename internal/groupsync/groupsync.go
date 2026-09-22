// Package groupsync projects master data's vehicle groups into fleet_groups.
//
// FMS fleet groups and TMS truck groups were two unrelated ways of saying
// "this depot's trucks". Master data owns them now (catalogue kind
// vehicleGroup), as it owns vehicles, drivers and devices: this projects the
// groups, vehiclesync projects the membership (Truck.truck_group_id), and
// the Fleet Groups page and the vehicle form write through.
//
// Shape mirrors the other projections: one transaction per company as that
// tenant; pair by master_data_id; adopt an existing local group by
// normalised name once; insert; keep name and description in step; and when
// master data has retired a group, ungroup its vehicles and drop it — a
// deleted group is not worth an archived row.
package groupsync

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/catalog"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/tenantdir"
)

// Syncer projects groups for every FMS company.
type Syncer struct {
	pool *db.Pool
	dir  *tenantdir.Directory
	md   *mdclient.Client
	wake chan struct{}
}

// New returns nil unless both halves are configured, like vehiclesync.
func New(pool *db.Pool, dir *tenantdir.Directory, md *mdclient.Client) *Syncer {
	if dir == nil || md == nil {
		return nil
	}
	return &Syncer{pool: pool, dir: dir, md: md, wake: make(chan struct{}, 1)}
}

// Wake asks for a refresh now (a karlo:masterdata notice, or an operator).
func (s *Syncer) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run refreshes now, then on a ticker or whenever woken.
func (s *Syncer) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 5 * time.Minute
	}
	if err := s.pass(ctx); err != nil {
		log.Printf("groupsync: first refresh failed, groups keep the existing rows: %v", err)
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
		if err := s.pass(ctx); err != nil {
			log.Printf("groupsync: %v", err)
		}
	}
}

const passTimeout = 3 * time.Minute

func (s *Syncer) pass(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, passTimeout)
	defer cancel()
	start := time.Now()
	err := s.Once(ctx)
	if d := time.Since(start); d > 30*time.Second {
		log.Printf("groupsync: pass took %s", d.Round(time.Second))
	}
	return err
}

// Result is what one pass changed.
type Result struct {
	Companies int
	Seen      int
	Linked    int
	Inserted  int
	Updated   int
	Removed   int // retired upstream: vehicles ungrouped, row dropped
}

// Once reconciles every company's groups.
func (s *Syncer) Once(ctx context.Context) error {
	companies, err := s.dir.List(ctx)
	if err != nil {
		return err
	}
	if len(companies) == 0 {
		log.Printf("groupsync: the company list came back empty; leaving the projection alone")
		return nil
	}
	var total Result
	for _, c := range companies {
		if c.CompanyID == "" || c.ID == 0 {
			continue
		}
		r, err := s.company(ctx, c)
		if err != nil {
			log.Printf("groupsync: %s (tenant %d): %v", c.Name, c.ID, err)
			continue
		}
		total.Companies++
		total.Seen += r.Seen
		total.Linked += r.Linked
		total.Inserted += r.Inserted
		total.Updated += r.Updated
		total.Removed += r.Removed
	}
	if total.Linked+total.Inserted+total.Updated+total.Removed > 0 {
		log.Printf("groupsync: %d group(s) across %d companies — %d linked, %d added, %d updated, %d removed",
			total.Seen, total.Companies, total.Linked, total.Inserted, total.Updated, total.Removed)
	}
	return nil
}

func (s *Syncer) company(ctx context.Context, c tenantdir.Tenant) (Result, error) {
	var r Result
	groups, err := s.md.ListTruckGroups(ctx, c.CompanyID)
	if err != nil {
		return r, err
	}
	r.Seen = len(groups)
	if len(groups) == 0 {
		return r, nil // no groups and a failed read look alike; do nothing
	}
	err = db.Tx(ctx, s.pool, c.ID, func(tx pgx.Tx) error {
		if err := db.LockFleetProjection(ctx, tx, c.ID); err != nil {
			return err
		}
		for _, g := range groups {
			name := strings.TrimSpace(g.Name)
			if g.Deleted {
				// Ungroup its vehicles, then drop the group (FK is RESTRICT).
				var id int64
				if err := tx.QueryRow(ctx, `SELECT id FROM fleet_groups WHERE master_data_id = $1`, g.ID).Scan(&id); err != nil {
					if err == pgx.ErrNoRows {
						continue
					}
					return err
				}
				if _, err := tx.Exec(ctx, `UPDATE vehicles SET fleet_group_id = NULL, updated_at = now() WHERE fleet_group_id = $1`, id); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM fleet_groups WHERE id = $1`, id); err != nil {
					return err
				}
				r.Removed++
				continue
			}
			if name == "" {
				continue
			}
			// 1. Adopt by normalised name, once.
			ct, err := tx.Exec(ctx, `
				UPDATE fleet_groups SET master_data_id = $1, updated_at = now()
				 WHERE tenant_id = $2 AND master_data_id IS NULL
				   AND lower(regexp_replace(name, '[^A-Za-z0-9]', '', 'g')) = $3`,
				g.ID, c.ID, catalog.Normalise(name))
			if err != nil {
				return err
			}
			r.Linked += int(ct.RowsAffected())
			// 2. Insert when unknown.
			ct, err = tx.Exec(ctx, `
				INSERT INTO fleet_groups (tenant_id, name, description, master_data_id)
				SELECT $1, $2, NULLIF($3,''), $4
				 WHERE NOT EXISTS (SELECT 1 FROM fleet_groups w WHERE w.master_data_id = $4)`,
				c.ID, name, g.Description, g.ID)
			if err != nil {
				return err
			}
			r.Inserted += int(ct.RowsAffected())
			// 3. Keep name and description in step.
			ct, err = tx.Exec(ctx, `
				UPDATE fleet_groups SET name = $2, description = COALESCE(NULLIF($3,''), description), updated_at = now()
				 WHERE master_data_id = $1
				   AND (name IS DISTINCT FROM $2 OR (NULLIF($3,'') IS NOT NULL AND description IS DISTINCT FROM $3))`,
				g.ID, name, g.Description)
			if err != nil {
				return err
			}
			r.Updated += int(ct.RowsAffected())
		}
		return nil
	})
	return r, err
}
