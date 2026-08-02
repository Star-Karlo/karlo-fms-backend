// Package retention keeps the hot application database from growing without
// bound. Unlike telemetry (which has a Parquet cold tier), the app DB has no
// archival of its own, so every append-only table — alerts, driving events,
// webhook delivery logs, expired sessions — accumulates forever unless swept.
//
// Two kinds of table are handled:
//
//   - Zero-value logs (webhook_deliveries, expired refresh_tokens and
//     tracking_links, dead pending/failed video_clips rows) are deleted
//     outright — nothing about them is worth keeping past its window.
//   - Medium-value history (alerts, driving_events) is first written to the
//     cold S3 bucket as gzipped NDJSON (one JSON object per row, via
//     row_to_json so it is schema-drift-proof and fully restorable), and only
//     then deleted. If the cold bucket is not configured these two are left
//     alone — the sweep never deletes what it could not archive.
//
// The sweep runs per tenant inside the normal RLS-scoped Tx, so it can only
// ever touch one tenant's rows at a time, and deletes in bounded batches to
// avoid a single long-held lock on the fleet's largest tables.
package retention

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/coldstore"
	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
)

// batchSize bounds both the delete-loop chunk and the archive read/upload chunk.
// Small enough that a batch's lock is brief and a batch's rows fit comfortably
// in memory while being gzipped; large enough that a big first sweep converges.
const batchSize = 2000

// startupDelay lets the service finish booting (and any migration open the DB)
// before the first sweep, so retention never competes with startup.
const startupDelay = 5 * time.Minute

// Sweeper deletes aged rows on a schedule. Construct with New and run Run in a
// goroutine for the life of the process.
type Sweeper struct {
	pool *db.Pool
	cfg  config.RetentionConfig
	cold *coldstore.Store // nil when cold storage is unconfigured
}

// New builds a Sweeper. A nil cold store (COLD_BUCKET unset) still runs the
// zero-value prunes, but skips the archive-then-delete tables so nothing is
// deleted without first being archived.
func New(pool *db.Pool, cold *coldstore.Store, cfg config.RetentionConfig) *Sweeper {
	return &Sweeper{pool: pool, cfg: cfg, cold: cold}
}

// Run sweeps once shortly after startup, then every cfg.Interval, until ctx is
// cancelled. A disabled config makes Run a no-op.
func (s *Sweeper) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		log.Printf("retention: disabled (RETENTION_ENABLED=false)")
		return
	}
	if s.cold == nil {
		log.Printf("retention: COLD_BUCKET unset — pruning zero-value tables only; alerts/driving_events kept")
	}
	timer := time.NewTimer(startupDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := s.sweepOnce(ctx); err != nil {
				log.Printf("retention: sweep: %v", err)
			}
			timer.Reset(s.cfg.Interval)
		}
	}
}

func (s *Sweeper) sweepOnce(ctx context.Context) error {
	tenants, err := s.listTenants(ctx)
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	for _, tid := range tenants {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.sweepTenant(ctx, tid)
	}
	return nil
}

// listTenants reads every tenant id. Runs without a tenant scope (like the
// notification engine) purely to enumerate; all mutation below is per-tenant.
func (s *Sweeper) listTenants(ctx context.Context) ([]int64, error) {
	var ids []int64
	err := db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM tenants`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return rows.Err()
	})
	return ids, err
}

func (s *Sweeper) sweepTenant(ctx context.Context, tid int64) {
	c := s.cfg
	if c.WebhookDays > 0 {
		s.pruneBatched(ctx, tid, "webhook_deliveries", delWebhook, daysAgo(c.WebhookDays))
	}
	if c.RefreshTokenDays > 0 {
		s.pruneBatched(ctx, tid, "refresh_tokens", delRefresh, daysAgo(c.RefreshTokenDays))
	}
	if c.TrackingLinkDays > 0 {
		s.pruneBatched(ctx, tid, "tracking_links", delLink, daysAgo(c.TrackingLinkDays))
	}
	if c.DeadClipDays > 0 {
		s.pruneBatched(ctx, tid, "video_clips (dead)", delDeadClip, daysAgo(c.DeadClipDays))
	}
	if c.AlertDays > 0 {
		s.archiveAndPrune(ctx, tid, "alerts", "occurred_at", daysAgo(c.AlertDays))
	}
	if c.DrivingEventDays > 0 {
		s.archiveAndPrune(ctx, tid, "driving_events", "occurred_at", daysAgo(c.DrivingEventDays))
	}
}

// pruneBatched runs deleteSQL (a self-limiting DELETE taking $1 = cutoff)
// repeatedly until a batch removes fewer than batchSize rows. Each batch is its
// own transaction so locks are released between chunks.
func (s *Sweeper) pruneBatched(ctx context.Context, tid int64, label, deleteSQL string, cutoff time.Time) {
	var total int64
	for {
		if ctx.Err() != nil {
			return
		}
		var n int64
		err := db.Tx(ctx, s.pool, tid, func(tx pgx.Tx) error {
			ct, err := tx.Exec(ctx, deleteSQL, cutoff)
			if err != nil {
				return err
			}
			n = ct.RowsAffected()
			return nil
		})
		if err != nil {
			log.Printf("retention: tenant %d: %s: %v", tid, label, err)
			return
		}
		total += n
		if n < batchSize {
			break
		}
	}
	if total > 0 {
		log.Printf("retention: tenant %d: pruned %d from %s", tid, total, label)
	}
}

// archiveAndPrune moves rows older than cutoff to the cold bucket as gzipped
// NDJSON, then deletes exactly those rows by id. It never deletes a row it did
// not first upload: an upload failure returns without deleting, so the same
// rows are retried on the next sweep (a re-run only creates a duplicate,
// harmless archive object). Skipped entirely when no S3 client is configured.
func (s *Sweeper) archiveAndPrune(ctx context.Context, tid int64, table, timeCol string, cutoff time.Time) {
	if s.cold == nil {
		return
	}
	// table/timeCol are package-internal literals (see sweepTenant), never
	// request-derived, so interpolating them into SQL carries no injection risk.
	readSQL := fmt.Sprintf(
		"SELECT id, %s, row_to_json(t) FROM %s t WHERE %s < $1 ORDER BY %s LIMIT %d",
		timeCol, table, timeCol, timeCol, batchSize)
	delSQL := fmt.Sprintf("DELETE FROM %s WHERE id = ANY($1)", table)

	var total int64
	for {
		if ctx.Err() != nil {
			return
		}
		var (
			ids    []int64
			buf    bytes.Buffer
			oldest time.Time
		)
		gz := gzip.NewWriter(&buf)
		err := db.Tx(ctx, s.pool, tid, func(tx pgx.Tx) error {
			rows, err := tx.Query(ctx, readSQL, cutoff)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var (
					id int64
					ts time.Time
					js string
				)
				if err := rows.Scan(&id, &ts, &js); err != nil {
					return err
				}
				if oldest.IsZero() {
					oldest = ts
				}
				ids = append(ids, id)
				if _, err := gz.Write([]byte(js + "\n")); err != nil {
					return err
				}
			}
			return rows.Err()
		})
		if err != nil {
			log.Printf("retention: tenant %d: read %s: %v", tid, table, err)
			return
		}
		if len(ids) == 0 {
			break
		}
		if err := gz.Close(); err != nil {
			log.Printf("retention: tenant %d: gzip %s: %v", tid, table, err)
			return
		}

		key := coldstore.Key(table, tid, oldest, time.Now().UnixNano())
		if err := s.cold.Put(ctx, &s3.PutObjectInput{
			Bucket:       aws.String(s.cold.Bucket()),
			Key:          aws.String(key),
			Body:         bytes.NewReader(buf.Bytes()),
			ContentType:  aws.String("application/gzip"),
			StorageClass: s3types.StorageClassGlacierIr, // born cold, instant-retrieval
		}); err != nil {
			log.Printf("retention: tenant %d: upload %s: %v (rows kept, will retry)", tid, table, err)
			return
		}

		var deleted int64
		err = db.Tx(ctx, s.pool, tid, func(tx pgx.Tx) error {
			ct, err := tx.Exec(ctx, delSQL, ids)
			if err != nil {
				return err
			}
			deleted = ct.RowsAffected()
			return nil
		})
		if err != nil {
			log.Printf("retention: tenant %d: delete %s: %v (already archived to %s)", tid, table, err, key)
			return
		}
		total += deleted
		if len(ids) < batchSize {
			break
		}
	}
	if total > 0 {
		log.Printf("retention: tenant %d: archived+pruned %d from %s", tid, total, table)
	}
}

// daysAgo is the cutoff instant n days before now.
func daysAgo(n int) time.Time {
	return time.Now().Add(-time.Duration(n) * 24 * time.Hour)
}

// --- Restore ---------------------------------------------------------------

// RestoreResult summarizes a restore run.
type RestoreResult struct {
	Table    string `json:"table"`
	Found    int    `json:"found"`    // rows read from the archive
	Inserted int64  `json:"inserted"` // rows brought back
	Skipped  int64  `json:"skipped"`  // rows already present or unrestorable (FK)
}

// restorable maps the archive-then-delete tables to their conflict key. Only
// these are archived, so only these can be restored.
var restorable = map[string]bool{"alerts": true, "driving_events": true}

// Restore pulls archived rows for `table` whose date-partition falls within
// [from,to] back into the DB for tenantID. It is idempotent: rows whose id
// already exists are skipped (ON CONFLICT DO NOTHING), so re-running is safe and
// a row that was never deleted is simply left as-is. Requires a cold bucket.
func (s *Sweeper) Restore(ctx context.Context, tenantID int64, table string, from, to time.Time) (RestoreResult, error) {
	res := RestoreResult{Table: table}
	if s.cold == nil {
		return res, fmt.Errorf("cold bucket not configured")
	}
	if !restorable[table] {
		return res, fmt.Errorf("table %q is not archived", table)
	}

	lines, err := s.cold.Lines(ctx, table, tenantID, from, to)
	if err != nil {
		return res, err
	}
	res.Found = len(lines)

	// Set-based insert per chunk; fall back to row-by-row only if a chunk fails
	// (e.g. an alert whose vehicle was later hard-deleted), so one bad row never
	// blocks the rest.
	insBatch := fmt.Sprintf(
		"INSERT INTO %s OVERRIDING SYSTEM VALUE "+
			"SELECT (jsonb_populate_record(NULL::%s, elem)).* "+
			"FROM jsonb_array_elements($1::jsonb) AS elem ON CONFLICT (id) DO NOTHING", table, table)
	insOne := fmt.Sprintf(
		"INSERT INTO %s OVERRIDING SYSTEM VALUE "+
			"SELECT (jsonb_populate_record(NULL::%s, $1::jsonb)).* ON CONFLICT (id) DO NOTHING", table, table)

	for start := 0; start < len(lines); start += batchSize {
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		end := start + batchSize
		if end > len(lines) {
			end = len(lines)
		}
		chunk := lines[start:end]

		var ins int64
		err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
			ct, err := tx.Exec(ctx, insBatch, jsonArray(chunk))
			if err != nil {
				return err
			}
			ins = ct.RowsAffected()
			return nil
		})
		if err != nil {
			log.Printf("retention: restore %s: chunk failed (%v); retrying row-by-row", table, err)
			ri, rs := s.restoreRows(ctx, tenantID, insOne, chunk)
			res.Inserted += ri
			res.Skipped += rs
			continue
		}
		res.Inserted += ins
		res.Skipped += int64(len(chunk)) - ins
	}
	log.Printf("retention: restore tenant %d %s [%s..%s]: %d rows read, %d inserted, %d skipped",
		tenantID, table, coldstore.DayOf(from).Format("2006-01-02"), coldstore.DayOf(to).Format("2006-01-02"),
		res.Found, res.Inserted, res.Skipped)
	return res, nil
}

// restoreRows inserts rows one at a time (the chunk fallback). A row that fails
// (a broken FK, malformed JSON) is counted as skipped rather than aborting.
func (s *Sweeper) restoreRows(ctx context.Context, tenantID int64, insOne string, rows []json.RawMessage) (inserted, skipped int64) {
	for _, ln := range rows {
		var n int64
		err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
			ct, err := tx.Exec(ctx, insOne, string(ln))
			if err != nil {
				return err
			}
			n = ct.RowsAffected()
			return nil
		})
		if err != nil {
			skipped++
			continue
		}
		inserted += n
		skipped += 1 - n // a no-op insert (conflict) counts as skipped
	}
	return inserted, skipped
}

// jsonArray wraps NDJSON rows into a single JSON array string for a set-based
// insert. Each element is already valid JSON, so joining with commas is safe.
func jsonArray(rows []json.RawMessage) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(r)
	}
	b.WriteByte(']')
	return b.String()
}

// Self-limiting DELETEs. batchSize is a compile-time constant; $1 is the cutoff.
// The IN (SELECT ... LIMIT) form keeps each statement bounded regardless of how
// far behind the sweep is.
var (
	delWebhook = fmt.Sprintf(
		`DELETE FROM webhook_deliveries WHERE id IN (`+
			`SELECT id FROM webhook_deliveries WHERE created_at < $1 LIMIT %d)`, batchSize)

	// Expired sessions, plus sessions revoked more than the window ago. A
	// still-valid token revoked yesterday is left until its window elapses.
	delRefresh = fmt.Sprintf(
		`DELETE FROM refresh_tokens WHERE id IN (`+
			`SELECT id FROM refresh_tokens `+
			`WHERE expires_at < $1 OR (revoked_at IS NOT NULL AND revoked_at < $1) LIMIT %d)`, batchSize)

	delLink = fmt.Sprintf(
		`DELETE FROM tracking_links WHERE id IN (`+
			`SELECT id FROM tracking_links WHERE expires_at < $1 LIMIT %d)`, batchSize)

	// Only clips that never completed — 'ready' evidence is never touched here
	// (its lifecycle is the S3 bucket's job).
	delDeadClip = fmt.Sprintf(
		`DELETE FROM video_clips WHERE id IN (`+
			`SELECT id FROM video_clips WHERE status IN ('pending','failed') AND created_at < $1 LIMIT %d)`, batchSize)
)
