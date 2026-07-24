package dashcam

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/vss"
)

// Sync refreshes cached online state for one tenant's linked dashcams.
//
// Deliberately ONE batched status call rather than one per device: the vendor
// endpoint takes a comma-separated list, and a fleet of cameras polled
// individually would be a request per device per tick.
//
// Only the cache is updated — never the link. A device that has vanished from
// VSS keeps its row and simply stops reporting online, because silently
// unlinking a camera during an outage would lose the installer's work.
func Sync(ctx context.Context, pool *db.Pool, client *vss.Client, tenantID int64) (int, error) {
	if !client.Enabled() {
		return 0, vss.ErrNotConfigured
	}
	var ids []string
	if err := db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT vss_device_id FROM dashcams WHERE status='active'`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	}); err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	statuses, err := client.DeviceStatus(ctx, ids...)
	if err != nil {
		return 0, err
	}

	updated := 0
	err = db.Tx(ctx, pool, tenantID, func(tx pgx.Tx) error {
		for _, s := range statuses {
			// last_online_at only moves forward while the device IS online;
			// otherwise every poll of an offline unit would rewrite it and
			// "last seen" would always read as now.
			ct, e := tx.Exec(ctx, `
UPDATE dashcams
   SET access_mode = $2,
       last_online_at = CASE WHEN $3 THEN now() ELSE last_online_at END,
       last_sync_at = now(), updated_at = now()
 WHERE vss_device_id = $1`, s.DeviceID, s.AccessMode, s.Online())
			if e != nil {
				return e
			}
			updated += int(ct.RowsAffected())
		}
		return nil
	})
	return updated, err
}

// Poller keeps every tenant's dashcam state fresh in the background, the same
// shape as telemetrysync. Without it a camera that went offline overnight still
// reads "online" until somebody opens the page and forces a sync.
type Poller struct {
	pool   *db.Pool
	client *vss.Client
	every  time.Duration
}

// NewPoller builds the background poller. A minute is frequent enough for a
// state that only changes when a truck starts, stops, or loses signal.
func NewPoller(pool *db.Pool, client *vss.Client) *Poller {
	return &Poller{pool: pool, client: client, every: time.Minute}
}

// Run polls until ctx is cancelled. A no-op when VSS is not configured, so a
// deployment without cameras carries no background traffic at all.
func (p *Poller) Run(ctx context.Context) {
	if !p.client.Enabled() {
		return
	}
	t := time.NewTicker(p.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.tick(ctx)
		}
	}
}

func (p *Poller) tick(ctx context.Context) {
	var tenants []int64
	// `tenants` carries no RLS policy, so this reads unscoped; the per-tenant
	// work below runs inside that tenant's own transaction.
	rows, err := p.pool.Query(ctx, `SELECT id FROM tenants ORDER BY id`)
	if err != nil {
		log.Printf("dashcam poller: tenants: %v", err)
		return
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			tenants = append(tenants, id)
		}
	}
	rows.Close()

	for _, tid := range tenants {
		if _, err := Sync(ctx, p.pool, p.client, tid); err != nil {
			// Logged, not fatal: one tenant's camera platform being unreachable
			// must not stop the others being polled.
			log.Printf("dashcam poller: tenant %d: %v", tid, err)
		}
	}
}
