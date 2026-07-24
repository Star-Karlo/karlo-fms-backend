-- 0028_dashcams.sql — link a Gtrack VSS dashcam (MDVR) to a vehicle.
--
-- WHAT LIVES HERE AND WHAT DOES NOT. The video itself never enters this
-- database — VSS stores it, and the browser streams from VSS directly. This
-- table holds only the LINK (which VSS device is on which truck) plus the
-- cached state a fleet screen needs before deciding to open a stream: is it
-- online, when was it last heard from, how many cameras has it got.
--
-- The join key is the VSS deviceID, exactly as telemetry joins on tracker IMEI.
-- They are different identifiers on different networks: a truck can have a GPS
-- tracker and no dashcam, a dashcam and no tracker, or both — so this is its own
-- table rather than more columns on `trackers`.
--
-- Idempotent.

CREATE TABLE IF NOT EXISTS dashcams (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id      bigint NOT NULL REFERENCES tenants(id),
  -- Nullable: a device can be registered in VSS before anyone knows which truck
  -- it was fitted to. An unlinked dashcam is visible in the install flow as
  -- "not assigned" rather than being invisible until someone guesses.
  vehicle_id     bigint REFERENCES vehicles(id) ON DELETE SET NULL,
  vss_device_id  text NOT NULL,
  device_name    text,
  device_type    text,
  -- Channel count and per-channel labels ("Front;Cabin"). VSS returns these as
  -- semicolon-separated names; stored as written so the UI can show "Cabin"
  -- rather than "channel 2".
  channels       int NOT NULL DEFAULT 0,
  channel_names  text,
  -- Cached from VSS by the vsssync poller. accessmode follows the vendor's
  -- device-status table (-1 offline, 1 ethernet, 5 4G, …) rather than a
  -- boolean, because "offline" and "online over 2G" are different operational
  -- situations when you are about to ask for video.
  access_mode    int,
  last_online_at timestamptz,
  last_sync_at   timestamptz,
  status         text NOT NULL DEFAULT 'active',
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

-- One row per device per tenant; re-running the sync upserts rather than piling
-- up duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS dashcams_tenant_device_uk
  ON dashcams (tenant_id, vss_device_id);

-- A truck has at most one dashcam. Partial, so many rows may sit unassigned.
CREATE UNIQUE INDEX IF NOT EXISTS dashcams_vehicle_uk
  ON dashcams (tenant_id, vehicle_id) WHERE vehicle_id IS NOT NULL;

-- Tenant isolation, matching every other business table: rows visible only when
-- the tenant is set AND matches. Unset tenant sees nothing.
ALTER TABLE dashcams ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON dashcams;
CREATE POLICY tenant_isolation ON dashcams
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON dashcams TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;

DO $$ BEGIN RAISE NOTICE 'dashcams ready'; END $$;
