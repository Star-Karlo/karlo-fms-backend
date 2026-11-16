-- name: CreateVehicle :one
INSERT INTO vehicles (
  tenant_id, license_plate, manufacturer, vehicle_type, box_type, unit_year, hull_no,
  fleet_group_id, status, garage, odometer, hourmeter, tracker_id, current_driver_id, notes,
  fuel_tank_liters, fuel_ratio_kmpl, master_data_id,
  axles, golongan_override, length_m, width_m, height_m, max_weight_kg
) VALUES (
  $1, $2, sqlc.narg('manufacturer'), sqlc.narg('vehicle_type'), sqlc.narg('box_type'),
  sqlc.narg('unit_year'), sqlc.narg('hull_no'), sqlc.narg('fleet_group_id'),
  COALESCE(sqlc.narg('status'), 'active'), sqlc.narg('garage'), sqlc.narg('odometer'),
  sqlc.narg('hourmeter'), sqlc.narg('tracker_id'), sqlc.narg('current_driver_id'), sqlc.narg('notes'),
  sqlc.narg('fuel_tank_liters'), sqlc.narg('fuel_ratio_kmpl'), sqlc.narg('master_data_id'),
  sqlc.narg('axles'), sqlc.narg('golongan_override'),
  sqlc.narg('length_m'), sqlc.narg('width_m'), sqlc.narg('height_m'),
  sqlc.narg('max_weight_kg')
)
RETURNING *;

-- name: GetVehicleRow :one
-- The bare row, without GetVehicle's joins. Used where only the vehicle's own
-- columns matter — notably master_data_id, read inside the same transaction as
-- a write so the upstream mirror targets the record this row is paired with.
SELECT * FROM vehicles WHERE id = $1;

-- name: GetVehicleBehaviorConfig :one
-- What the behaviour report echoes about the vehicle itself.
SELECT license_plate, hull_no, fuel_ratio_kmpl, fuel_tank_liters FROM vehicles WHERE id = $1;

-- name: ListVehiclesByHull :many
-- Matched loosely, like plates: case and separators are ignored, because a
-- hull number is transcribed with whatever grouping the typist chose.
SELECT id, license_plate, status FROM vehicles
 WHERE upper(regexp_replace(hull_no, '[^A-Za-z0-9]', '', 'g')) = sqlc.arg(hull)::text
 ORDER BY id;

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
    fuel_ratio_kmpl   = COALESCE(sqlc.narg('fuel_ratio_kmpl'), fuel_ratio_kmpl),
    -- COALESCE, like every field above: an omitted value leaves what is there.
    -- It matters more for these than for most, because master data will start
    -- sending them later and vehiclesync uses the same rule — so a figure
    -- entered here is not erased by either path.
    axles             = COALESCE(sqlc.narg('axles'), axles),
    golongan_override = COALESCE(sqlc.narg('golongan_override'), golongan_override),
    length_m          = COALESCE(sqlc.narg('length_m'), length_m),
    width_m           = COALESCE(sqlc.narg('width_m'), width_m),
    height_m          = COALESCE(sqlc.narg('height_m'), height_m),
    max_weight_kg     = COALESCE(sqlc.narg('max_weight_kg'), max_weight_kg)
WHERE id = $1
RETURNING *;

-- name: AssignVehicleTracker :one
UPDATE vehicles SET tracker_id = sqlc.narg('tracker_id') WHERE id = $1 RETURNING *;

-- name: AssignVehicleDriver :one
UPDATE vehicles SET current_driver_id = sqlc.narg('current_driver_id') WHERE id = $1 RETURNING *;

-- name: ArchiveVehicle :exec
UPDATE vehicles SET status = 'archive' WHERE id = $1;

-- Hard delete, the second step of a two-stage delete: a truck already
-- archived is removed for good. The FMS rows that name it have to go first —
-- alerts, service schedules and score events reference it with no ON DELETE
-- clause, so they would block the delete, and sop_runs and documents carry no
-- foreign key at all and would be left pointing at an id that no longer
-- exists. The rest the database handles itself: telemetry state, geofence
-- membership, rule attachments and the tracker fit history cascade away;
-- dashcams, video clips and driving events keep their row and lose the link.
--
-- Telemetry (positions, trips) lives in the other database keyed by IMEI and
-- is NOT touched by any of this.

-- name: DeleteVehicleAlerts :execrows
DELETE FROM alerts WHERE vehicle_id = $1;

-- name: DeleteVehicleServiceSchedules :execrows
DELETE FROM service_schedules WHERE vehicle_id = $1;

-- name: DeleteVehicleScoreEvents :execrows
DELETE FROM driver_score_events WHERE vehicle_id = $1;

-- name: DeleteVehicleSopRuns :execrows
DELETE FROM sop_runs WHERE vehicle_id = $1;

-- name: DeleteVehicleDocuments :execrows
DELETE FROM documents WHERE owner_type = 'vehicle' AND owner_id = $1;

-- name: DeleteVehicle :execrows
DELETE FROM vehicles WHERE id = $1;

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
  -- Tank size: FMS's own field (master data does not hold it), published
  -- here because every consumer showing a vehicle's specification needs it
  -- and the alternative is a second request per truck.
  v.fuel_tank_liters,
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
-- Retired trucks are off the map. 'repair' stays on it: a truck in the
-- workshop is still a truck somebody drives there and back, and the registry
-- status travels with the row so the panel can say why it is not working.
-- Only 'archive' means deleted.
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
  -- Tank size: FMS's own field (master data does not hold it), published
  -- here because every consumer showing a vehicle's specification needs it
  -- and the alternative is a second request per truck.
  v.fuel_tank_liters,
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
