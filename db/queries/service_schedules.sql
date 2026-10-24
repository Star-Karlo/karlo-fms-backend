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
  day_interval, day_reminder, last_done_at, last_odometer, next_due_at, next_odometer, status,
  hangar_id
) VALUES (
  $1, sqlc.narg('vehicle_id'), sqlc.narg('component'), sqlc.narg('scheduler'),
  sqlc.narg('km_interval'), sqlc.narg('km_reminder'), sqlc.narg('day_interval'), sqlc.narg('day_reminder'),
  sqlc.narg('last_done_at'), sqlc.narg('last_odometer'), sqlc.narg('next_due_at'), sqlc.narg('next_odometer'),
  COALESCE(sqlc.narg('status'), 'normal'),
  sqlc.narg('hangar_id')
)
RETURNING *;

-- name: DeleteServiceSchedule :exec
DELETE FROM service_schedules WHERE id = $1;

-- name: GetServiceSchedule :one
-- RLS scopes this to the caller's tenant (0055), so an id from another tenant
-- returns no rows rather than another company's maintenance plan.
SELECT s.*, v.license_plate, v.status AS vehicle_status
FROM service_schedules s
LEFT JOIN vehicles v ON v.id = s.vehicle_id
WHERE s.id = $1;

-- name: DispatchServiceSchedule :one
-- Send a vehicle to a hangar for this service.
--
-- Guarded in SQL rather than in the handler: the WHERE clause makes
-- dispatching an already-dispatched schedule return no rows instead of
-- overwriting when and where it was first sent. Two operators clicking at once
-- then produce one dispatch and one "already in service", which is the honest
-- answer to what happened.
UPDATE service_schedules
SET status               = 'in_service',
    hangar_id            = sqlc.arg('hangar_id'),
    dispatched_at        = now(),
    dispatched_by        = sqlc.narg('dispatched_by'),
    dispatch_distance_km = sqlc.narg('distance_km'),
    dispatch_eta_min     = sqlc.narg('eta_min')
WHERE id = sqlc.arg('id')
  AND status <> 'in_service'
RETURNING *;

-- name: CompleteServiceSchedule :one
-- Work finished. Records what was done and rolls the schedule forward.
--
-- next_due_at and next_odometer are passed in rather than computed here: the
-- interval rules live in Go alongside the ones that set them at creation, and
-- splitting that arithmetic across two languages is how the two drift apart.
UPDATE service_schedules
SET status               = 'normal',
    last_done_at         = COALESCE(sqlc.narg('done_at')::date, CURRENT_DATE),
    last_odometer        = COALESCE(sqlc.narg('odometer'), last_odometer),
    next_due_at          = sqlc.narg('next_due_at'),
    next_odometer        = sqlc.narg('next_odometer'),
    dispatched_at        = NULL,
    dispatched_by        = NULL,
    dispatch_distance_km = NULL,
    dispatch_eta_min     = NULL
WHERE id = sqlc.arg('id')
  AND status = 'in_service'
RETURNING *;

-- name: SetVehicleStatus :exec
-- Used by dispatch and complete to take a truck off the road and put it back.
--
-- Never touches 'archive': a retired vehicle that happens to carry an open
-- schedule must not be silently returned to the fleet by completing it.
UPDATE vehicles
SET status = sqlc.arg('status'), updated_at = now()
WHERE id = sqlc.arg('id') AND status <> 'archive';
