-- A clip is identified by camera + channel + window. The Save button and the
-- history map look one up by exactly that (dashcam.lookupClip) before
-- starting a download, so a re-click or a refreshed page reuses the clip
-- instead of pulling the footage again.
CREATE INDEX IF NOT EXISTS video_clips_window_idx
  ON video_clips (tenant_id, vss_device_id, channel, clip_start);
