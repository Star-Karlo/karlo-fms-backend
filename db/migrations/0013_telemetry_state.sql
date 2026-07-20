-- 0013_telemetry_state.sql — per-vehicle telemetry accumulator state.
-- Written by the background telemetry collector (internal/telemetrysync):
--   * engine_hours_seconds — cumulative ignition-on time (engine hours)
--   * last_odometer_km     — latest telemetry "Total Odometer" (also synced to
--                            vehicles.odometer so km-based maintenance advances)
--   * last_voltage_mv      — latest external (vehicle battery) voltage, for
--                            sudden-drop / disconnect (tamper) alerts
--   * inside_geofences     — geofence ids the vehicle is currently inside, for
--                            enter/exit alert transitions
-- One row per (tenant, vehicle). Idempotent; RLS matches 0012.

CREATE TABLE IF NOT EXISTS vehicle_telemetry_state (
  id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id            bigint NOT NULL REFERENCES tenants(id),
  vehicle_id           bigint NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  engine_hours_seconds bigint NOT NULL DEFAULT 0,
  last_ignition        boolean,
  last_odometer_km     numeric,
  last_voltage_mv      numeric,
  last_gsm_signal      numeric,
  last_lat             double precision,
  last_lon             double precision,
  last_sample_at       timestamptz,
  inside_geofences     jsonb NOT NULL DEFAULT '[]',
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, vehicle_id)
);

-- Safe re-run: the gsm column was added after the table's first cut.
ALTER TABLE vehicle_telemetry_state ADD COLUMN IF NOT EXISTS last_gsm_signal numeric;

ALTER TABLE vehicle_telemetry_state ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON vehicle_telemetry_state;
CREATE POLICY tenant_isolation ON vehicle_telemetry_state
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON vehicle_telemetry_state TO fms_app;

-- Geofence ↔ vehicle assignment: a vehicle assigned to a geofence gets
-- Enter/Exit alerts automatically (no separate notification rule needed).
CREATE TABLE IF NOT EXISTS geofence_vehicles (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  geofence_id bigint NOT NULL REFERENCES geofences(id) ON DELETE CASCADE,
  vehicle_id  bigint NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, geofence_id, vehicle_id)
);

ALTER TABLE geofence_vehicles ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON geofence_vehicles;
CREATE POLICY tenant_isolation ON geofence_vehicles
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON geofence_vehicles TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
