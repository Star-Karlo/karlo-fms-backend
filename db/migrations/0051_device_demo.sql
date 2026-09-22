-- 0051_device_demo.sql — demo copies of a device.
--
-- The same physical tracker or dashcam is registered under several FMS
-- accounts on purpose (a customer's unit also shown in the DEMO account).
-- Master data holds ONE owner per device; the copies elsewhere are demo
-- rows: kept for the demo, badged in the register, never paired with or
-- written through to master data. trackersync sets the flag from master
-- data's owner (a row in another account is demo); a platform admin may
-- also set it by hand.
ALTER TABLE trackers ADD COLUMN IF NOT EXISTS is_demo boolean NOT NULL DEFAULT false;
ALTER TABLE dashcams ADD COLUMN IF NOT EXISTS is_demo boolean NOT NULL DEFAULT false;
