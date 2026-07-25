-- 0029_video_clips.sql — durable record of dashcam clips pulled to S3.
--
-- The video bytes live in karlo-fms-cold-storage/video/ (written by the
-- telemetry service). This table is how FMS finds them: one row per saved clip,
-- with the metadata a person searches by — which truck, when, and where the
-- dashcam was at the time. Accident evidence, basically.
--
-- The clip's COORDINATE is stored, not linked: it is the dashcam's own GPS at
-- the moment of recording, resolved once by the telemetry service at download
-- time. Keeping it here means the evidence record stands on its own even if the
-- GPS history later ages out of the hot window.
--
-- Idempotent.

CREATE TABLE IF NOT EXISTS video_clips (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  vehicle_id    bigint REFERENCES vehicles(id) ON DELETE SET NULL,
  vss_device_id text   NOT NULL,
  channel       int    NOT NULL,
  -- The recorded time window, from the clip itself (not when it was saved).
  clip_start    timestamptz NOT NULL,
  clip_stop     timestamptz,
  -- Where the S3 object lives. Bucket kept explicitly so a later bucket change
  -- does not orphan old rows.
  s3_bucket     text NOT NULL,
  s3_key        text NOT NULL,
  size_bytes    bigint,
  -- Dashcam GPS at clip_start, resolved at download time. Nullable: the track
  -- may not have covered that instant.
  lat           double precision,
  lon           double precision,
  -- Why it was kept: 'manual' (operator downloaded it) or an event code later.
  reason        text NOT NULL DEFAULT 'manual',
  note          text,
  created_by    bigint REFERENCES users(id) ON DELETE SET NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- The same clip should not be saved twice.
CREATE UNIQUE INDEX IF NOT EXISTS video_clips_key_uk ON video_clips (tenant_id, s3_key);
-- "show me this truck's footage" is the common lookup.
CREATE INDEX IF NOT EXISTS video_clips_vehicle_idx ON video_clips (tenant_id, vehicle_id, clip_start DESC);

ALTER TABLE video_clips ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON video_clips;
CREATE POLICY tenant_isolation ON video_clips
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON video_clips TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;

DO $$ BEGIN RAISE NOTICE 'video_clips ready'; END $$;
