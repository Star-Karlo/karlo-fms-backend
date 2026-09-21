-- name: CreateVehicle :one
INSERT INTO vehicles (
  tenant_id, license_plate, manufacturer, vehicle_type, box_type, unit_year, hull_no,
  fleet_group_id, status, garage, odometer, hourmeter, tracker_id, current_driver_id, notes,
  fuel_tank_liters, fuel_ratio_kmpl, master_data_id
) VALUES (
  $1, $2, sqlc.narg('manufacturer'), sqlc.narg('vehicle_type'), sqlc.narg('box_type'),
  sqlc.narg('unit_year'), sqlc.narg('hull_no'), sqlc.narg('fleet_group_id'),
  COALESCE(sqlc.narg('status'), 'active'), sqlc.narg('garage'), sqlc.narg('odometer'),
  sqlc.narg('hourmeter'), sqlc.narg('tracker_id'), sqlc.narg('current_driver_id'), sqlc.narg('notes'),
  sqlc.narg('fuel_tank_liters'), sqlc.narg('fuel_ratio_kmpl'), sqlc.narg('master_data_id')
)
RETURNING *;

-- name: GetVehicleRow :one
-- The bare row, without GetVehicle's joins. Used where only the vehicle's own
-- columns matter — notably master_data_id, read inside the same transaction as
-- a write so the upstream mirror targets the record this row is paired with.
SELECT * FROM vehicles WHERE id = $1;

-- name: PairVehicle :one
-- Attach an existing local vehicle to its master-data record, for rows created
-- before write-through existed. Fills a NULL only: re-pointing a paired vehicle
-- would move its alerts, telemetry and history onto a different truck.
UPDATE vehicles SET master_data_id = $2, updated_at = now()
 WHERE id = $1 AND master_data_id IS NULL
RETURNING *;

-- name: ListUnpairedVehicles :many
-- Vehicles this company holds that master data has never seen: created in FMS
-- after the cutover, when writes still stopped at fms_app. Invisible to TMS
-- until the backfill pushes them upstream.
SELECT * FROM vehicles
 WHERE tenant_id = $1 AND master_data_id IS NULL AND status <> 'archive'
 ORDER BY id;

-- name: GetVehicle :one
SELECT
  v.*,
  fg.name        AS fleet_group_name,
  e.full_name    AS driver_name,
  e.phone        AS driver_phone,
  t.imei         AS tracker_imei,
  t.phone_no     AS tracker_phone
FROM vehicles v
LEFT JOIN fleet_groups fg ON fg.id = v.fleet_group_id
LEFT JOIN employees e     ON e.id = v.current_driver_id
LEFT JOIN trackers t      ON t.id = v.tracker_id
WHERE v.id = $1;

-- name: UpdateVehicle :one
UPDATE vehicles
SET license_plate     = COALESCE(sqlc.narg('license_plate'), license_plate),
    manufacturer      = COALESCE(sqlc.narg('manufacturer'), manufacturer),
    vehicle_type      = COALESCE(sqlc.narg('vehicle_type'), vehicle_type),
    box_type          = COALESCE(sqlc.narg('box_type'), box_type),
    unit_year         = COALESCE(sqlc.narg('unit_year'), unit_year),
    hull_no           = COALESCE(sqlc.narg('hull_no'), hull_no),
    fleet_group_id    = COALESCE(sqlc.narg('fleet_group_id'), fleet_group_id),
    status            = COALESCE(sqlc.narg('status'), status),
    garage            = COALESCE(sqlc.narg('garage'), garage),
    odometer          = COALESCE(sqlc.narg('odometer'), odometer),
    hourmeter         = COALESCE(sqlc.narg('hourmeter'), hourmeter),
    notes             = COALESCE(sqlc.narg('notes'), notes),
    fuel_tank_liters  = COALESCE(sqlc.narg('fuel_tank_liters'), fuel_tank_liters),
    fuel_ratio_kmpl   = COALESCE(sqlc.narg('fuel_ratio_kmpl'), fuel_ratio_kmpl)
WHERE id = $1
RETURNING *;

-- name: AssignVehicleTracker :one
UPDATE vehicles SET tracker_id = sqlc.narg('tracker_id') WHERE id = $1 RETURNING *;

-- name: AssignVehicleDriver :one
UPDATE vehicles SET current_driver_id = sqlc.narg('current_driver_id') WHERE id = $1 RETURNING *;

-- name: ArchiveVehicle :exec
UPDATE vehicles SET status = 'archive' WHERE id = $1;

-- name: ListVehicles :many
SELECT
  v.*,
  fg.name     AS fleet_group_name,
  e.full_name AS driver_name,
  t.imei      AS tracker_imei
FROM vehicles v
LEFT JOIN fleet_groups fg ON fg.id = v.fleet_group_id
LEFT JOIN employees e     ON e.id = v.current_driver_id
LEFT JOIN trackers t      ON t.id = v.tracker_id
WHERE (sqlc.narg('q')::text IS NULL
       OR regexp_replace(v.license_plate, '[-[:space:]]', '', 'g') ILIKE '%' || regexp_replace(sqlc.narg('q'), '[-[:space:]]', '', 'g') || '%'
       OR v.hull_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR v.status = sqlc.narg('status'))
  AND (sqlc.narg('fleet_group_id')::bigint IS NULL OR v.fleet_group_id = sqlc.narg('fleet_group_id'))
ORDER BY v.license_plate
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountVehicles :one
SELECT count(*) FROM vehicles v
WHERE (sqlc.narg('q')::text IS NULL
       OR regexp_replace(v.license_plate, '[-[:space:]]', '', 'g') ILIKE '%' || regexp_replace(sqlc.narg('q'), '[-[:space:]]', '', 'g') || '%'
       OR v.hull_no ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR v.status = sqlc.narg('status'))
  AND (sqlc.narg('fleet_group_id')::bigint IS NULL OR v.fleet_group_id = sqlc.narg('fleet_group_id'));

-- name: ListLiveViewVehicles :many
-- Active vehicles that can be located: those with a tracker IMEI, OR a linked
-- dashcam whose own GPS stands in when there is no tracker, OR an assigned
-- driver whose phone (K-Trip) can. The live-view
-- endpoint enriches each with a position — the tracker fix, else the camera
-- fix, else the assigned driver's phone.
SELECT
  v.id            AS vehicle_id,
  v.license_plate,
  v.vehicle_type,
  v.status,
  -- The master-data vehicle id, for callers that key on the shared fleet
  -- register rather than on this database's own ids. NULL for a row the
  -- projection has not paired yet.
  v.master_data_id,
  fg.name         AS fleet_group_name,
  e.id            AS driver_id,
  e.full_name     AS driver_name,
  e.phone         AS driver_phone,
  -- The driver's master-data id is what the K-Trip phone app reports its
  -- positions under: the third position source, after tracker and camera.
  COALESCE(e.master_data_id, '') AS driver_master_data_id,
  t.id            AS tracker_id,
  t.imei          AS tracker_imei,
  COALESCE(dc.vss_device_id, '') AS camera_device_id
FROM vehicles v
LEFT JOIN trackers t      ON t.id = v.tracker_id
-- At most one camera per vehicle stands in for GPS; pick the lowest-id device.
LEFT JOIN LATERAL (
  SELECT vss_device_id FROM dashcams d WHERE d.vehicle_id = v.id ORDER BY d.id LIMIT 1
) dc ON true
LEFT JOIN fleet_groups fg ON fg.id = v.fleet_group_id
LEFT JOIN employees e     ON e.id = v.current_driver_id
WHERE v.status <> 'archive'
  AND (t.imei IS NOT NULL OR dc.vss_device_id IS NOT NULL OR e.master_data_id IS NOT NULL)
  AND (sqlc.narg('fleet_group_id')::bigint IS NULL OR v.fleet_group_id = sqlc.narg('fleet_group_id'));

-- name: GetLiveViewVehicle :one
SELECT
  v.id            AS vehicle_id,
  v.license_plate,
  v.vehicle_type,
  v.status,
  v.master_data_id,
  fg.name         AS fleet_group_name,
  e.id            AS driver_id,
  e.full_name     AS driver_name,
  e.phone         AS driver_phone,
  -- The driver's master-data id is what the K-Trip phone app reports its
  -- positions under: the third position source, after tracker and camera.
  COALESCE(e.master_data_id, '') AS driver_master_data_id,
  t.id            AS tracker_id,
  t.imei          AS tracker_imei,
  COALESCE(dc.vss_device_id, '') AS camera_device_id
FROM vehicles v
LEFT JOIN trackers t      ON t.id = v.tracker_id
LEFT JOIN LATERAL (
  SELECT vss_device_id FROM dashcams d WHERE d.vehicle_id = v.id ORDER BY d.id LIMIT 1
) dc ON true
LEFT JOIN fleet_groups fg ON fg.id = v.fleet_group_id
LEFT JOIN employees e     ON e.id = v.current_driver_id
WHERE v.id = $1;
