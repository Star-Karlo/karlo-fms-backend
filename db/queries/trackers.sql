-- name: CreateTracker :one
INSERT INTO trackers (tenant_id, imei, phone_no, vehicle_id, status)
VALUES ($1, $2, sqlc.narg('phone_no'), sqlc.narg('vehicle_id'), COALESCE(sqlc.narg('status'), 'active'))
RETURNING *;

-- name: GetTracker :one
SELECT * FROM trackers WHERE id = $1;

-- name: UpdateTracker :one
UPDATE trackers
SET imei       = COALESCE(sqlc.narg('imei'), imei),
    phone_no   = COALESCE(sqlc.narg('phone_no'), phone_no),
    status     = COALESCE(sqlc.narg('status'), status)
WHERE id = $1
RETURNING *;

-- name: SetTrackerVehicle :one
UPDATE trackers SET vehicle_id = sqlc.narg('vehicle_id') WHERE id = $1
RETURNING *;

-- name: DeleteTracker :exec
DELETE FROM trackers WHERE id = $1;

-- name: ListTrackers :many
SELECT * FROM trackers
WHERE (sqlc.narg('q')::text IS NULL
       OR imei ILIKE '%' || sqlc.narg('q') || '%'
       OR phone_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('vehicle_id')::bigint IS NULL OR vehicle_id = sqlc.narg('vehicle_id'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountTrackers :one
SELECT count(*) FROM trackers
WHERE (sqlc.narg('q')::text IS NULL
       OR imei ILIKE '%' || sqlc.narg('q') || '%'
       OR phone_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('vehicle_id')::bigint IS NULL OR vehicle_id = sqlc.narg('vehicle_id'));

-- name: ClearTrackerByVehicle :exec
-- Detach whatever tracker currently points at this vehicle. Used on UNBIND so the
-- change to trackers.vehicle_id fires the assignment-history trigger (closes the
-- open period) — the bind path already syncs the tracker side, unbind did not.
UPDATE trackers SET vehicle_id = NULL WHERE vehicle_id = $1;

-- name: ClearOtherTrackersOnVehicle :exec
-- On BIND: detach any OTHER tracker still pointing at this vehicle (fixes the
-- bidirectional staleness so a vehicle never has two open device assignments).
UPDATE trackers SET vehicle_id = NULL WHERE vehicle_id = $1 AND id <> $2;
