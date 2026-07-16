-- name: ListServiceSchedules :many
SELECT s.*, v.license_plate, v.vehicle_type, v.odometer AS current_odometer, v.hourmeter AS current_hourmeter
FROM service_schedules s
LEFT JOIN vehicles v ON v.id = s.vehicle_id
WHERE (sqlc.narg('q')::text IS NULL OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%' OR s.component ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status'))
ORDER BY s.next_due_at ASC NULLS LAST
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountServiceSchedules :one
SELECT count(*) FROM service_schedules s
LEFT JOIN vehicles v ON v.id = s.vehicle_id
WHERE (sqlc.narg('q')::text IS NULL OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%' OR s.component ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR s.status = sqlc.narg('status'));

-- name: ServiceStatusCounts :one
SELECT
  count(*) FILTER (WHERE status = 'overdue') AS overdue,
  count(*) FILTER (WHERE status = 'due_soon') AS due_soon,
  count(*) FILTER (WHERE status = 'in_service') AS in_service,
  count(*) FILTER (WHERE status = 'normal') AS normal
FROM service_schedules;

-- name: CreateServiceSchedule :one
INSERT INTO service_schedules (
  tenant_id, vehicle_id, component, scheduler, km_interval, km_reminder,
  day_interval, day_reminder, last_done_at, last_odometer, next_due_at, next_odometer, status
) VALUES (
  $1, sqlc.narg('vehicle_id'), sqlc.narg('component'), sqlc.narg('scheduler'),
  sqlc.narg('km_interval'), sqlc.narg('km_reminder'), sqlc.narg('day_interval'), sqlc.narg('day_reminder'),
  sqlc.narg('last_done_at'), sqlc.narg('last_odometer'), sqlc.narg('next_due_at'), sqlc.narg('next_odometer'),
  COALESCE(sqlc.narg('status'), 'normal')
)
RETURNING *;

-- name: DeleteServiceSchedule :exec
DELETE FROM service_schedules WHERE id = $1;
