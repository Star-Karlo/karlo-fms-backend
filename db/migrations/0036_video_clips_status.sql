-- 0036_video_clips_status.sql — clips get a lifecycle so a save can show up in
-- Saved Evidence immediately (pending) and finish in the background.
--
-- A clip is recorded the moment the operator clicks Save, as 'pending' with the
-- job id; a server-side reconciler flips it to 'ready' (with the S3 key) or
-- 'failed' when the download finishes. So s3_bucket/s3_key are no longer known
-- at insert time and become nullable.
ALTER TABLE video_clips ADD COLUMN IF NOT EXISTS status text NOT NULL DEFAULT 'ready';
ALTER TABLE video_clips ADD COLUMN IF NOT EXISTS job_id text;
ALTER TABLE video_clips ADD COLUMN IF NOT EXISTS error  text;
ALTER TABLE video_clips ALTER COLUMN s3_bucket DROP NOT NULL;
ALTER TABLE video_clips ALTER COLUMN s3_key    DROP NOT NULL;

-- Existing rows are finished downloads → 'ready' (the column default already set
-- them). Find in-flight ones fast.
CREATE INDEX IF NOT EXISTS video_clips_pending_idx ON video_clips (status) WHERE status = 'pending';
