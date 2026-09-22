-- 0052_fleet_group_master_data.sql — pair fleet groups with master data's
-- vehicle groups (catalogue kind vehicleGroup). Master data owns them now,
-- like vehicles, drivers and devices: groupsync projects them here, and
-- the Fleet Groups page writes through.
ALTER TABLE fleet_groups ADD COLUMN IF NOT EXISTS master_data_id text;
CREATE UNIQUE INDEX IF NOT EXISTS fleet_groups_master_data_id_key ON fleet_groups (master_data_id) WHERE master_data_id IS NOT NULL;
