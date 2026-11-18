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
--
-- per_event puts each event in exactly one outcome, so with video + camera didn't send it +
-- save failed + still downloading always adds up to events, however many
-- cameras the event has.
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.channel, c.vehicle_id, c.reason, c.status, c.size_bytes, c.clip_start
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
),
per_event AS (
  SELECT vss_device_id, alarm_id, max(vehicle_id) AS vehicle_id,
         count(*) FILTER (WHERE status = 'ready')                        AS clips,
         COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)    AS bytes,
         count(*) FILTER (WHERE status = 'failed')                       AS failed,
         count(*) FILTER (WHERE status = 'no_clip')                      AS no_clip,
         count(*) FILTER (WHERE status = 'pending')                      AS pending,
         CASE WHEN bool_or(status = 'ready')   THEN 'with_video'
              WHEN bool_or(status = 'pending') THEN 'downloading'
              WHEN bool_or(status = 'no_clip') THEN 'camera_didnt_send'
              ELSE 'save_failed' END                                     AS outcome
  FROM latest
  WHERE status <> 'deleted'
  GROUP BY vss_device_id, alarm_id
)
SELECT count(*)::bigint                       AS events,
       COALESCE(sum(clips), 0)::bigint        AS clips,
       COALESCE(sum(bytes), 0)::bigint        AS bytes,
       COALESCE(sum(failed), 0)::bigint       AS failed,
       COALESCE(sum(no_clip), 0)::bigint      AS no_clip,
       COALESCE(sum(pending), 0)::bigint      AS pending,
       count(DISTINCT vehicle_id)::bigint     AS trucks,
       count(*) FILTER (WHERE outcome = 'with_video')::bigint   AS events_with_video,
       count(*) FILTER (WHERE outcome = 'camera_didnt_send')::bigint     AS events_camera_didnt_send,
       count(*) FILTER (WHERE outcome = 'save_failed')::bigint  AS events_save_failed
FROM per_event;

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
-- reason is "auto:<event code>" on every automatic save, so it is the event
-- type. An SD-card pull linked to an event is "manual": the event keeps the
-- type of its automatic rows, so a pull never adds a "manual" type. The type is
-- read over all of the event's rows, before latest drops the older ones: a pull
-- that replaces a failed save on the same camera is that camera's latest row.
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.reason, c.status, c.size_bytes,
         max(c.reason) FILTER (WHERE c.reason LIKE 'auto:%') OVER (PARTITION BY c.vss_device_id, c.alarm_id) AS auto_reason
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
),
per_event AS (
  SELECT COALESCE(max(auto_reason), max(reason)) AS reason,
         count(*) FILTER (WHERE status = 'ready')                        AS clips,
         COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)    AS bytes,
         count(*) FILTER (WHERE status = 'failed')                       AS failed,
         count(*) FILTER (WHERE status = 'no_clip')                      AS no_clip,
         count(*) FILTER (WHERE status = 'pending')                      AS pending,
         CASE WHEN bool_or(status = 'ready')   THEN 'with_video'
              WHEN bool_or(status = 'pending') THEN 'downloading'
              WHEN bool_or(status = 'no_clip') THEN 'camera_didnt_send'
              ELSE 'save_failed' END                                     AS outcome
  FROM latest
  WHERE status <> 'deleted'
  GROUP BY vss_device_id, alarm_id
)
SELECT reason::text                  AS reason,
       count(*)::bigint              AS events,
       sum(clips)::bigint            AS clips,
       sum(bytes)::bigint            AS bytes,
       sum(failed)::bigint           AS failed,
       sum(no_clip)::bigint          AS no_clip,
       count(*) FILTER (WHERE outcome = 'with_video')::bigint   AS events_with_video,
       count(*) FILTER (WHERE outcome = 'camera_didnt_send')::bigint     AS events_camera_didnt_send,
       count(*) FILTER (WHERE outcome = 'save_failed')::bigint  AS events_save_failed
FROM per_event
GROUP BY reason
ORDER BY events DESC, reason;

-- name: AutoClipSummaryByVehicle :many
WITH latest AS (
  SELECT DISTINCT ON (c.vss_device_id, c.alarm_id, c.channel)
         c.vss_device_id, c.alarm_id, c.channel, c.vehicle_id, c.reason, c.status, c.size_bytes, c.clip_start
  FROM video_clips c
  WHERE c.alarm_id IS NOT NULL
    AND c.clip_start >= sqlc.arg('from_ts') AND c.clip_start < sqlc.arg('to_ts')
    AND (sqlc.narg('vehicle_id')::bigint IS NULL OR c.vehicle_id = sqlc.narg('vehicle_id'))
  ORDER BY c.vss_device_id, c.alarm_id, c.channel, c.created_at DESC, c.id DESC
),
per_event AS (
  SELECT vehicle_id, max(clip_start) AS last_start,
         count(*) FILTER (WHERE status = 'ready')                        AS clips,
         COALESCE(sum(size_bytes) FILTER (WHERE status = 'ready'), 0)    AS bytes,
         count(*) FILTER (WHERE status = 'failed')                       AS failed,
         count(*) FILTER (WHERE status = 'no_clip')                      AS no_clip,
         count(*) FILTER (WHERE status = 'pending')                      AS pending,
         CASE WHEN bool_or(status = 'ready')   THEN 'with_video'
              WHEN bool_or(status = 'pending') THEN 'downloading'
              WHEN bool_or(status = 'no_clip') THEN 'camera_didnt_send'
              ELSE 'save_failed' END                                     AS outcome
  FROM latest
  WHERE status <> 'deleted'
  GROUP BY vss_device_id, alarm_id, vehicle_id
)
SELECT a.vehicle_id,
       v.license_plate,
       count(*)::bigint                    AS events,
       sum(a.clips)::bigint                AS clips,
       sum(a.bytes)::bigint                AS bytes,
       sum(a.failed)::bigint               AS failed,
       sum(a.no_clip)::bigint              AS no_clip,
       max(a.last_start)::timestamptz      AS last_event,
       count(*) FILTER (WHERE a.outcome = 'with_video')::bigint   AS events_with_video,
       count(*) FILTER (WHERE a.outcome = 'camera_didnt_send')::bigint     AS events_camera_didnt_send,
       count(*) FILTER (WHERE a.outcome = 'save_failed')::bigint  AS events_save_failed
FROM per_event a
LEFT JOIN vehicles v ON v.id = a.vehicle_id
GROUP BY a.vehicle_id, v.license_plate
ORDER BY v.license_plate NULLS LAST;
