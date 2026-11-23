-- 0065_video_clips_alarm_id.sql — automatic dashcam saves record which camera
-- event a clip belongs to.
--
-- Camera + channel + clip_start is not enough to identify an event's clip: one
-- camera fired 21 events in the same second in production. alarm_id is the
-- VSS event id; manual saves leave it NULL.
--
-- One live row per event + camera. Failed rows are left out so a retry can add
-- a new row (the auto-save gives up after 3); 'deleted' rows stay in, so a clip
-- a user removed is never saved again. 'no_clip' rows use channel 0.
--
-- Additive: older code ignores the column. Idempotent.
ALTER TABLE video_clips ADD COLUMN IF NOT EXISTS alarm_id text;

CREATE UNIQUE INDEX IF NOT EXISTS video_clips_alarm_uk
  ON video_clips (tenant_id, vss_device_id, alarm_id, channel)
  WHERE alarm_id IS NOT NULL AND status <> 'failed';

DO $$ BEGIN RAISE NOTICE 'video_clips.alarm_id ready'; END $$;
