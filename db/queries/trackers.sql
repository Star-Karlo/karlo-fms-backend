-- name: CreateTracker :one
INSERT INTO trackers (tenant_id, imei, phone_no, vehicle_id, status, master_data_id)
VALUES ($1, $2, sqlc.narg('phone_no'), sqlc.narg('vehicle_id'), COALESCE(sqlc.narg('status'), 'active'), sqlc.narg('master_data_id'))
RETURNING *;

-- name: GetTracker :one
SELECT * FROM trackers WHERE id = $1;

-- name: UpdateTracker :one
UPDATE trackers
SET imei       = COALESCE(sqlc.narg('imei'), imei),
    phone_no   = COALESCE(sqlc.narg('phone_no'), phone_no),
    status     = COALESCE(sqlc.narg('status'), status),
    is_demo    = COALESCE(sqlc.narg('is_demo'), is_demo)
WHERE id = $1
RETURNING *;

-- name: SetTrackerVehicle :one
UPDATE trackers SET vehicle_id = sqlc.narg('vehicle_id') WHERE id = $1
RETURNING *;

-- name: DeleteTracker :exec
DELETE FROM trackers WHERE id = $1;

-- name: ListTrackers :many
-- The plate comes along because "which truck is this device on" is the first
-- thing anyone asks of a device register, and an id answers it only for
-- somebody holding a second screen.
SELECT t.*, v.license_plate AS vehicle_plate
FROM trackers t
LEFT JOIN vehicles v ON v.id = t.vehicle_id
WHERE (sqlc.narg('q')::text IS NULL
       OR t.imei ILIKE '%' || sqlc.narg('q') || '%'
       OR t.phone_no ILIKE '%' || sqlc.narg('q') || '%'
       OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR t.status = sqlc.narg('status'))
  AND (sqlc.narg('vehicle_id')::bigint IS NULL OR t.vehicle_id = sqlc.narg('vehicle_id'))
ORDER BY t.created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: GetTrackerWithVehicle :one
SELECT t.*, v.license_plate AS vehicle_plate
FROM trackers t
LEFT JOIN vehicles v ON v.id = t.vehicle_id
WHERE t.id = $1;

-- name: UnassignTracker :one
-- Take the device off whatever truck it is on, keeping the device. Both
-- sides are cleared: vehicles.tracker_id is what the live map reads, and
-- trackers.vehicle_id is what the assignment-history trigger watches, so
-- clearing only one leaves the pair disagreeing.
UPDATE trackers SET vehicle_id = NULL, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ClearVehicleTracker :exec
UPDATE vehicles SET tracker_id = NULL, updated_at = now() WHERE tracker_id = $1;

-- name: DeactivateTracker :exec
-- A device handed to another company: kept here, inactive, so its fit
-- history (and the telemetry attributed through it) stays readable.
UPDATE trackers SET status = 'inactive', vehicle_id = NULL, updated_at = now() WHERE id = $1;

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

-- name: StampTrackerMasterDataID :exec
-- Pair a local device with its master-data record, once.
UPDATE trackers SET master_data_id = $2 WHERE id = $1 AND master_data_id IS NULL;
