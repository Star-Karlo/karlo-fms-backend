-- 0031_driving_events.sql — harsh driving-behaviour events from the MDVR
-- G-sensor, the tenant-scoped record FMS scores drivers on.
--
-- The telemetry service receives every device alarm over the VSS WebSocket and
-- stores it raw (dashcam_alarms). FMS polls those, and for the DRIVING-BEHAVIOUR
-- ones — harsh braking, harsh acceleration, harsh cornering — materialises a row
-- here, mapped to the vehicle it owns and the driver on it at ingest time.
--
-- This is the source the driver-score collector reads to fill the harsh-braking
-- / acceleration / cornering pillars that internal/driverscore reserved but
-- could not measure from 90-second GPS. One row per real event; idempotent on
-- the telemetry alarm id so a re-poll never double-counts.
--
-- AXIS / KIND / SEVERITY:
--   axis     — X longitudinal (brake/accel), Y lateral (cornering),
--              Z vertical (road shock; stored for road-condition analysis, not
--              charged to the driver).
--   kind     — braking | acceleration | cornering | road_shock.
--   severity — yellow | red | black by peak G (0.3 / 0.4 / 0.8 g), or derived
--              from the event code when no G magnitude was reported yet.
--
-- Idempotent.

CREATE TABLE IF NOT EXISTS driving_events (
  id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id          bigint NOT NULL REFERENCES tenants(id),
  vehicle_id         bigint REFERENCES vehicles(id) ON DELETE SET NULL,
  -- Driver on the vehicle when the event was ingested (snapshot, like alerts).
  -- The score collector re-attributes by CURRENT driver, so this is provenance,
  -- not the scoring key.
  driver_id          bigint REFERENCES employees(id) ON DELETE SET NULL,
  -- Where the event came from: 'camera' (Howen/VSS MDVR G-sensor) or 'teltonika'
  -- (tracker Green Driving / Eco IO). Scoring is source-agnostic; this is
  -- provenance and lets the two ingests dedup independently. A truck with only a
  -- GPS tracker still scores — the camera is not required.
  source             text   NOT NULL DEFAULT 'camera',
  -- The device this came from: VSS deviceno for a camera, tracker id/IMEI for a
  -- Teltonika unit.
  vss_device_id      text   NOT NULL,
  -- The raw-layer source row id this was derived from — the idempotency key,
  -- unique per source.
  telemetry_alarm_id bigint NOT NULL,
  event_code         int    NOT NULL,   -- the raw VSS device-event code
  kind               text   NOT NULL,   -- braking | acceleration | cornering | road_shock
  axis               text,              -- X | Y | Z
  gforce             double precision,  -- peak magnitude, when reported
  severity           text   NOT NULL DEFAULT 'yellow', -- yellow | red | black
  occurred_at        timestamptz NOT NULL,
  lat                double precision,
  lon                double precision,
  speed_kph          double precision,
  created_at         timestamptz NOT NULL DEFAULT now()
);

-- Never count the same source row twice for a tenant. Keyed by source too, so a
-- camera alarm id and a Teltonika event id can never collide.
CREATE UNIQUE INDEX IF NOT EXISTS driving_events_alarm_uk
  ON driving_events (tenant_id, source, telemetry_alarm_id);
-- The scorer scans a period per vehicle; the detail view reads per driver.
CREATE INDEX IF NOT EXISTS driving_events_scan_idx
  ON driving_events (tenant_id, occurred_at);
CREATE INDEX IF NOT EXISTS driving_events_vehicle_idx
  ON driving_events (tenant_id, vehicle_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS driving_events_driver_idx
  ON driving_events (tenant_id, driver_id, occurred_at DESC);

ALTER TABLE driving_events ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON driving_events;
CREATE POLICY tenant_isolation ON driving_events
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON driving_events TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;

DO $$ BEGIN RAISE NOTICE 'driving_events ready'; END $$;
