package dashcam

import (
	"context"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// pendingClip is what a download job is for — the truck/time/coordinate a clip
// is recorded against. Carried from the "start download" call into the pending
// video_clips row, so the client cannot forge them onto someone else's footage.
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
}

// insertPendingClip records a clip as 'pending' the moment Save is clicked, so
// it shows in Saved Evidence immediately. The reconciler finishes it.
func (h *Handler) insertPendingClip(ctx context.Context, pc pendingClip, jobID string) (int64, error) {
	start, _ := time.Parse(clipStartLayout, pc.start)
	var stop *time.Time
	if t, err := time.Parse(clipStartLayout, pc.stop); err == nil {
		stop = &t
	}
	var id int64
	err := db.Tx(ctx, h.pool, pc.tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start, clip_stop,
                         status, job_id, lat, lon, reason, note, created_by)
VALUES ($1,$2,$3,$4,$5,$6,'pending',$7,$8,$9,'manual',NULLIF($10,''),$11)
RETURNING id`,
			pc.tenantID, pc.vehicleID, pc.deviceID, pc.channel, start, stop,
			jobID, pc.lat, pc.lon, pc.note, nullableUser(pc.userID)).Scan(&id)
	})
	return id, err
}

// clipStartLayout is the clock string the download request carries (camera-
// local, no zone). Parsed the same way here and in insertPendingClip so a
// lookup matches what a save stored.
const clipStartLayout = "2006-01-02 15:04:05"

// lookupClip finds the most recent clip saved for this camera + channel +
// start — the identity of a clip, whatever reason or note it was saved with.
// nil when none.
func (h *Handler) lookupClip(ctx context.Context, tenantID int64, deviceID string, channel int, startStr string) (*ClipDTO, error) {
	start, err := time.Parse(clipStartLayout, startStr)
	if err != nil {
		return nil, nil
	}
	var c ClipDTO
	found := false
	err = db.Tx(ctx, h.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT c.id, c.vehicle_id, v.license_plate, c.vss_device_id, c.channel,
       to_char(c.clip_start, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       c.status, c.error, c.s3_bucket, c.s3_key, c.size_bytes, c.lat, c.lon, c.reason, c.note,
       to_char(c.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'), COALESCE(c.job_id, '')
FROM video_clips c LEFT JOIN vehicles v ON v.id = c.vehicle_id
WHERE c.vss_device_id = $1 AND c.channel = $2 AND c.clip_start = $3
ORDER BY (c.status = 'ready') DESC, (c.status = 'pending') DESC, c.created_at DESC
LIMIT 1`, deviceID, channel, start)
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			found = true
			return rows.Scan(&c.ID, &c.VehicleID, &c.LicensePlate, &c.VSSDeviceID, &c.Channel,
				&c.ClipStart, &c.Status, &c.Error, &c.S3Bucket, &c.S3Key, &c.SizeBytes, &c.Lat, &c.Lon, &c.Reason, &c.Note, &c.CreatedAt, &c.JobID)
		}
		return rows.Err()
	})
	if err != nil || !found {
		return nil, err
	}
	return &c, nil
}

// nullableUser maps a zero user id (unauthenticated/system) to NULL so the FK
// is not violated.
func nullableUser(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

// ReconcileClips finishes pending clip downloads server-side: it polls each
// in-flight job and flips its row to 'ready' (with the S3 key) or 'failed', so a
// save completes even after the operator has navigated away.
func (h *Handler) ReconcileClips(ctx context.Context) {
	t := time.NewTicker(8 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.reconcileOnce(ctx)
		}
	}
}

type pendingRow struct {
	id      int64
	jobID   string
	created time.Time
}

func (h *Handler) reconcileOnce(ctx context.Context) {
	// tenants carries no RLS policy, so it is read directly.
	var tenantIDs []int64
	rows, err := h.pool.Query(ctx, `SELECT id FROM tenants`)
	if err != nil {
		return
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			tenantIDs = append(tenantIDs, id)
		}
	}
	rows.Close()

	for _, tid := range tenantIDs {
		var pend []pendingRow
		_ = db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
			r, err := tx.Query(ctx, `SELECT id, job_id, created_at FROM video_clips
				WHERE status = 'pending' AND job_id IS NOT NULL LIMIT 200`)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var pr pendingRow
				if err := r.Scan(&pr.id, &pr.jobID, &pr.created); err == nil {
					pend = append(pend, pr)
				}
			}
			return r.Err()
		})
		for _, pr := range pend {
			h.reconcileClip(ctx, tid, pr)
		}
	}
}

// clipDeadline is how long a pending clip may run before it is given up on — a
// little past the telemetry service's own (20-minute) download deadline, so the
// reconciler never fails a job that is still legitimately in flight.
const clipDeadline = 23 * time.Minute

func (h *Handler) reconcileClip(ctx context.Context, tid int64, pr pendingRow) {
	st, err := h.tel.DashcamDownloadStatus(ctx, pr.jobID)
	if err != nil {
		// Job unknown — the telemetry service restarted or swept it. Give up only
		// once it's clearly not coming back.
		if time.Since(pr.created) > clipDeadline {
			h.failClip(ctx, tid, pr.id, "the download did not complete — please try again")
		}
		return
	}
	switch st.Status {
	case "done":
		if st.S3Key != "" {
			h.readyClip(ctx, tid, pr.id, st.Bucket, st.S3Key)
		}
	case "failed":
		msg := st.Error
		if msg == "" {
			msg = "the download failed"
		}
		h.failClip(ctx, tid, pr.id, msg)
	default: // downloading / uploading — still in flight
		if time.Since(pr.created) > clipDeadline {
			h.failClip(ctx, tid, pr.id, "the download timed out")
		}
	}
}

func (h *Handler) readyClip(ctx context.Context, tid, id int64, bucket, key string) {
	err := db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE video_clips
			SET status='ready', s3_bucket=$2, s3_key=$3, error=NULL
			WHERE id=$1 AND status='pending'`, id, bucket, key)
		return e
	})
	if err != nil {
		log.Printf("dashcam: mark clip %d ready: %v", id, err)
	}
}

func (h *Handler) failClip(ctx context.Context, tid, id int64, msg string) {
	err := db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE video_clips SET status='failed', error=$2
			WHERE id=$1 AND status='pending'`, id, msg)
		return e
	})
	if err != nil {
		log.Printf("dashcam: mark clip %d failed: %v", id, err)
	}
}
