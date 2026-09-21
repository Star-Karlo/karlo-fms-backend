-- 0050_tracker_master_data.sql — pair devices with master data.
--
-- Devices were the dataset the cutover missed: fms_app.trackers and
-- fms_app.dashcams kept their own life while master data grew a trackers
-- collection from a TMS backfill. Master data owns devices now (like
-- vehicles and drivers): trackersync projects them here, and the Trackers
-- page / dashcam link write through. master_data_id is the pairing.
ALTER TABLE trackers ADD COLUMN IF NOT EXISTS master_data_id text;
ALTER TABLE dashcams ADD COLUMN IF NOT EXISTS master_data_id text;
CREATE UNIQUE INDEX IF NOT EXISTS trackers_master_data_id_key ON trackers (master_data_id) WHERE master_data_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS dashcams_master_data_id_key ON dashcams (master_data_id) WHERE master_data_id IS NOT NULL;
