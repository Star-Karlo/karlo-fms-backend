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
