package dashcam

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// pendingClip is what a download job is for. Held server-side between the
// "start download" call and the poll that sees it finish, so the client does
// not have to send the truck/time/coordinate back to be recorded — and cannot
// forge them onto someone else's footage.
type pendingClip struct {
	tenantID  int64
	dashcamID int64
	vehicleID *int64
	deviceID  string
	channel   int
	start     string
	stop      string
	lat, lon  *float64
	note      string
	userID    int64
	created   time.Time
}

// remember stores the intent for a job id.
func (h *Handler) remember(jobID string, pc pendingClip) {
	pc.created = time.Now()
	h.pmu.Lock()
	if h.pending == nil {
		h.pending = map[string]pendingClip{}
	}
	// Cheap opportunistic sweep: drop anything older than an hour so a job that
	// is never polled to completion cannot leak.
	cutoff := time.Now().Add(-time.Hour)
	for id, p := range h.pending {
		if p.created.Before(cutoff) {
			delete(h.pending, id)
		}
	}
	h.pending[jobID] = pc
	h.pmu.Unlock()
}

// take removes and returns the intent for a job id, so a repeated poll after
// completion records the clip only once.
func (h *Handler) take(jobID string) (pendingClip, bool) {
	h.pmu.Lock()
	defer h.pmu.Unlock()
	pc, ok := h.pending[jobID]
	if ok {
		delete(h.pending, jobID)
	}
	return pc, ok
}

// recordClip writes the saved clip to video_clips, tenant-scoped. Idempotent on
// the S3 key so a race between two polls cannot double-insert.
func (h *Handler) recordClip(ctx context.Context, pc pendingClip, bucket, key string) (int64, error) {
	const layout = "2006-01-02 15:04:05"
	start, _ := time.Parse(layout, pc.start)
	var stop *time.Time
	if t, err := time.Parse(layout, pc.stop); err == nil {
		stop = &t
	}
	var id int64
	err := db.Tx(ctx, h.pool, pc.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start, clip_stop,
                         s3_bucket, s3_key, lat, lon, reason, note, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'manual',NULLIF($11,''),$12)
ON CONFLICT (tenant_id, s3_key) DO UPDATE SET note = EXCLUDED.note
RETURNING id`,
			pc.tenantID, pc.vehicleID, pc.deviceID, pc.channel, start, stop,
			bucket, key, pc.lat, pc.lon, pc.note, nullableUser(pc.userID)).Scan(&id)
	})
	return id, err
}

// nullableUser maps a zero user id (unauthenticated/system) to NULL so the FK
// is not violated.
func nullableUser(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
