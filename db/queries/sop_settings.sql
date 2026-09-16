-- name: GetSopSettings :one
-- Always answers: the row is created with the defaults on first read.
INSERT INTO company_sop_settings (tenant_id) VALUES (@tenant_id)
ON CONFLICT (tenant_id) DO UPDATE SET tenant_id = EXCLUDED.tenant_id
RETURNING *;

-- name: UpdateSopSettings :one
INSERT INTO company_sop_settings (tenant_id, sop_enabled, speed_limit_kmh, overspeed_min, stop_max_min, offline_min,
                                  sensor_timeout_min, idle_min, fuel_price_per_litre, idle_lph, updated_by, updated_at)
VALUES (@tenant_id, @sop_enabled, @speed_limit_kmh, @overspeed_min, @stop_max_min, @offline_min,
        @sensor_timeout_min, @idle_min, @fuel_price_per_litre, @idle_lph, sqlc.narg('updated_by'), now())
ON CONFLICT (tenant_id) DO UPDATE SET
  sop_enabled = EXCLUDED.sop_enabled, speed_limit_kmh = EXCLUDED.speed_limit_kmh, overspeed_min = EXCLUDED.overspeed_min,
  stop_max_min = EXCLUDED.stop_max_min, offline_min = EXCLUDED.offline_min, sensor_timeout_min = EXCLUDED.sensor_timeout_min,
  idle_min = EXCLUDED.idle_min, fuel_price_per_litre = EXCLUDED.fuel_price_per_litre, idle_lph = EXCLUDED.idle_lph,
  updated_by = EXCLUDED.updated_by, updated_at = now()
RETURNING *;

-- name: ListSopEnabledTenants :many
SELECT * FROM company_sop_settings WHERE sop_enabled;

-- name: ListTenantVehiclesWithTracker :many
-- Every active vehicle of the tenant with a tracker, for the SOP pass.
SELECT v.id AS vehicle_id, v.license_plate, t.imei, (v.fuel_tank_liters IS NOT NULL)::boolean AS has_fuel_sensor
FROM vehicles v JOIN trackers t ON t.id = v.tracker_id
WHERE v.tenant_id = @tenant_id AND v.status = 'active';
