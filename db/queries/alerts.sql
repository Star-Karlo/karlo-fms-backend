-- name: ListAlerts :many
SELECT
  a.*,
  v.license_plate,
  e.full_name AS driver_name
FROM alerts a
LEFT JOIN vehicles v  ON v.id = a.vehicle_id
LEFT JOIN employees e ON e.id = a.driver_id
WHERE (sqlc.narg('q')::text IS NULL OR a.alert_name ILIKE '%' || sqlc.narg('q') || '%' OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('check_status')::text IS NULL OR a.check_status = sqlc.narg('check_status'))
  AND (sqlc.narg('device_source')::text IS NULL OR a.device_source = sqlc.narg('device_source'))
  AND (sqlc.narg('severity')::text IS NULL OR a.severity = sqlc.narg('severity'))
  AND (sqlc.narg('alert_code')::text IS NULL OR a.alert_code = sqlc.narg('alert_code'))
  AND (sqlc.narg('vehicle_id')::bigint IS NULL OR a.vehicle_id = sqlc.narg('vehicle_id'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR a.occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR a.occurred_at <= sqlc.narg('to'))
ORDER BY a.occurred_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountAlerts :one
SELECT count(*) FROM alerts a
LEFT JOIN vehicles v ON v.id = a.vehicle_id
WHERE (sqlc.narg('q')::text IS NULL OR a.alert_name ILIKE '%' || sqlc.narg('q') || '%' OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('check_status')::text IS NULL OR a.check_status = sqlc.narg('check_status'))
  AND (sqlc.narg('device_source')::text IS NULL OR a.device_source = sqlc.narg('device_source'))
  AND (sqlc.narg('severity')::text IS NULL OR a.severity = sqlc.narg('severity'))
  AND (sqlc.narg('alert_code')::text IS NULL OR a.alert_code = sqlc.narg('alert_code'))
  AND (sqlc.narg('vehicle_id')::bigint IS NULL OR a.vehicle_id = sqlc.narg('vehicle_id'))
  AND (sqlc.narg('from')::timestamptz IS NULL OR a.occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR a.occurred_at <= sqlc.narg('to'));

-- name: GetAlert :one
SELECT a.*, v.license_plate, e.full_name AS driver_name
FROM alerts a
LEFT JOIN vehicles v  ON v.id = a.vehicle_id
LEFT JOIN employees e ON e.id = a.driver_id
WHERE a.id = $1;

-- name: UpdateAlertCheck :one
UPDATE alerts
SET check_status = $2,
    note         = COALESCE(sqlc.narg('note'), note),
    checked_at   = CASE WHEN $2 = 'unchecked' THEN NULL ELSE now() END
WHERE id = $1
RETURNING *;

-- name: AlertsInWindow :many
-- All alerts in a time window (bounded), for Control Tower aggregation in Go.
SELECT a.id, a.vehicle_id, a.alert_code, a.alert_name, a.severity, a.check_status, a.occurred_at
FROM alerts a
WHERE (sqlc.narg('from')::timestamptz IS NULL OR a.occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR a.occurred_at <= sqlc.narg('to'))
ORDER BY a.occurred_at DESC;

-- name: AlertHistoryForVehicle :many
-- Alerts for a vehicle within +/- a window (Notification Detail timeline).
SELECT a.id, a.alert_code, a.alert_name, a.severity, a.occurred_at, a.lat, a.lon
FROM alerts a
WHERE a.vehicle_id = $1
  AND a.occurred_at BETWEEN sqlc.arg('from') AND sqlc.arg('to')
ORDER BY a.occurred_at DESC;

-- name: AlertCountByType :many
SELECT alert_code, alert_name, count(*)::bigint AS count
FROM alerts
WHERE (sqlc.narg('from')::timestamptz IS NULL OR occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR occurred_at <= sqlc.narg('to'))
GROUP BY alert_code, alert_name ORDER BY count DESC;

-- name: AlertCountBySeverity :many
SELECT severity, count(*)::bigint AS count
FROM alerts
WHERE (sqlc.narg('from')::timestamptz IS NULL OR occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR occurred_at <= sqlc.narg('to'))
GROUP BY severity;

-- name: AlertCountByHour :many
SELECT EXTRACT(hour FROM occurred_at)::int AS hour, count(*)::bigint AS count
FROM alerts
WHERE (sqlc.narg('from')::timestamptz IS NULL OR occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR occurred_at <= sqlc.narg('to'))
GROUP BY hour ORDER BY hour;

-- name: AlertCountByDay :many
SELECT date_trunc('day', occurred_at)::date AS day, count(*)::bigint AS count
FROM alerts
WHERE (sqlc.narg('from')::timestamptz IS NULL OR occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR occurred_at <= sqlc.narg('to'))
GROUP BY day ORDER BY day;

-- name: AlertTopVehicles :many
SELECT a.vehicle_id, v.license_plate, count(*)::bigint AS count
FROM alerts a LEFT JOIN vehicles v ON v.id = a.vehicle_id
WHERE (sqlc.narg('from')::timestamptz IS NULL OR a.occurred_at >= sqlc.narg('from'))
  AND (sqlc.narg('to')::timestamptz IS NULL OR a.occurred_at <= sqlc.narg('to'))
GROUP BY a.vehicle_id, v.license_plate ORDER BY count DESC LIMIT 10;
