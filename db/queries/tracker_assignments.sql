-- Tracker↔vehicle assignment history: resolves telemetry (keyed by IMEI) to the
-- vehicle that actually carried the device at a given time, not the current one.
-- Both run under db.Tx (RLS scopes them to the caller's tenant).

-- name: VehicleTrackerPeriods :many
-- The device periods a vehicle carried that overlap [range_from, range_to): each
-- IMEI plus its fitted/unfitted window, so a historical read can fetch per period
-- and attribute only what THIS vehicle actually carried.
SELECT imei, fitted_at, unfitted_at
FROM tracker_assignments
WHERE vehicle_id = sqlc.arg('vehicle_id')
  AND fitted_at < sqlc.arg('range_to')
  AND (unfitted_at IS NULL OR unfitted_at > sqlc.arg('range_from'))
ORDER BY fitted_at;

-- name: VehicleAtTime :one
-- The vehicle that carried this IMEI at instant `at` (newest assignment covering
-- it). For attributing a single telemetry/event row to the right vehicle.
SELECT vehicle_id
FROM tracker_assignments
WHERE imei = sqlc.arg('imei')
  AND fitted_at <= sqlc.arg('at')
  AND (unfitted_at IS NULL OR unfitted_at > sqlc.arg('at'))
ORDER BY fitted_at DESC
LIMIT 1;
