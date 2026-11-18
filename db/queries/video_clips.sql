-- Automatic dashcam saves and the Saved Evidence summary. Every query runs in a
-- tenant transaction (db.Tx), so RLS scopes it to one customer.

-- name: AutosaveCameras :many
-- Demo copies are left out: the footage belongs to another account.
SELECT id, vss_device_id, vehicle_id
FROM dashcams
WHERE vehicle_id IS NOT NULL AND NOT is_demo
ORDER BY id;

-- name: AutoClipStates :many
-- Every row the auto-save already has for these events on one camera.
SELECT alarm_id, channel, status
FROM video_clips
WHERE vss_device_id = $1 AND alarm_id = ANY(sqlc.arg('alarm_ids')::text[]);

-- name: CountPendingAutoClips :one
SELECT count(*) FROM video_clips WHERE status = 'pending' AND alarm_id IS NOT NULL;

-- name: InsertNoClip :execrows
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start,
                         status, lat, lon, reason, alarm_id)
VALUES ($1, $2, $3, 0, $4, 'no_clip', $5, $6, $7, $8)
ON CONFLICT DO NOTHING;

-- name: DeleteNoClip :exec
DELETE FROM video_clips
WHERE vss_device_id = $1 AND alarm_id = $2 AND status = 'no_clip';

-- name: AutoClipSummary :one
-- Each event + camera counts once, by its latest row: a clip that failed twice
-- and then saved is one saved clip. clip_start holds WIB clock time labelled
-- UTC, so the window bounds are WIB midnights labelled UTC.
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.channel, c.vehicle_id, c.status, c.size_bytes
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
)
SELECT count(DISTINCT vss_device_id || '/' || alarm_id)::bigint                          AS events,
       count(*) FILTER (WHERE status = 'ready')::bigint                                  AS clips,
       COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)::bigint              AS bytes,
       count(*) FILTER (WHERE status = 'failed')::bigint                                 AS failed,
       count(*) FILTER (WHERE status = 'no_clip')::bigint                                AS no_clip,
       count(*) FILTER (WHERE status = 'pending')::bigint                                AS pending,
       count(DISTINCT vehicle_id)::bigint                                                AS trucks
FROM latest
WHERE status <> 'deleted';

-- name: AutoClipSummaryByDay :many
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.status, c.size_bytes,
         (c.clip_start AT TIME ZONE 'UTC')::date AS day
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
)
SELECT day::date                                                             AS day,
       count(DISTINCT vss_device_id || '/' || alarm_id)::bigint              AS events,
       count(*) FILTER (WHERE status = 'ready')::bigint                      AS clips,
       COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)::bigint  AS bytes,
       count(*) FILTER (WHERE status = 'failed')::bigint                     AS failed,
       count(*) FILTER (WHERE status = 'no_clip')::bigint                    AS no_clip
FROM latest
WHERE status <> 'deleted'
GROUP BY day
ORDER BY day;

-- name: AutoClipSummaryByType :many
-- reason is "auto:<event code>" on every automatic save, so it is the event type.
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.reason, c.status, c.size_bytes
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
)
SELECT reason,
       count(DISTINCT vss_device_id || '/' || alarm_id)::bigint              AS events,
       count(*) FILTER (WHERE status = 'ready')::bigint                      AS clips,
       COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)::bigint  AS bytes,
       count(*) FILTER (WHERE status = 'failed')::bigint                     AS failed,
       count(*) FILTER (WHERE status = 'no_clip')::bigint                    AS no_clip
FROM latest
WHERE status <> 'deleted'
GROUP BY reason
ORDER BY events DESC, reason;

-- name: AutoClipSummaryByVehicle :many
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.vehicle_id, c.status, c.size_bytes, c.clip_start
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
)
SELECT l.vehicle_id,
       v.license_plate,
       count(DISTINCT l.vss_device_id || '/' || l.alarm_id)::bigint              AS events,
       count(*) FILTER (WHERE l.status = 'ready')::bigint                        AS clips,
       COALESCE(sum(l.size_bytes) FILTER (WHERE l.status = 'ready'), 0)::bigint  AS bytes,
       count(*) FILTER (WHERE l.status = 'failed')::bigint                       AS failed,
       count(*) FILTER (WHERE l.status = 'no_clip')::bigint                      AS no_clip,
       max(l.clip_start)::timestamptz                                            AS last_event
FROM latest l
LEFT JOIN vehicles v ON v.id = l.vehicle_id
WHERE l.status <> 'deleted'
GROUP BY l.vehicle_id, v.license_plate
ORDER BY v.license_plate NULLS LAST;
