-- 0007_import_refs.sql — support importing external (legacy MongoDB) records.
--
-- Adds an external_ref (the source system's _id) to the registry tables so
-- imports are idempotent (re-running upserts instead of duplicating), and later
-- data drops (driver names, IMEIs) can be linked back by the same key.
--
-- Also relaxes the vehicle plate uniqueness: real fleet exports contain a few
-- duplicate plates, and external_ref is the true identity for imported rows.

-- Vehicles: external_ref, color, freeform attributes (dimensions, axle, class...).
ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS external_ref text;
ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS color       text;
ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS attributes  jsonb NOT NULL DEFAULT '{}';

-- Drop the strict plate unique constraint; keep a plain lookup index.
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_tenant_id_license_plate_key;
CREATE INDEX IF NOT EXISTS idx_vehicles_plate ON vehicles (tenant_id, license_plate);

-- external_ref uniqueness per tenant (the import idempotency key).
CREATE UNIQUE INDEX IF NOT EXISTS uq_vehicles_external_ref
  ON vehicles (tenant_id, external_ref) WHERE external_ref IS NOT NULL;

-- Same key on trackers + employees for when device/driver exports arrive.
ALTER TABLE trackers  ADD COLUMN IF NOT EXISTS external_ref text;
ALTER TABLE employees ADD COLUMN IF NOT EXISTS external_ref text;
CREATE UNIQUE INDEX IF NOT EXISTS uq_trackers_external_ref
  ON trackers (tenant_id, external_ref) WHERE external_ref IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uq_employees_external_ref
  ON employees (tenant_id, external_ref) WHERE external_ref IS NOT NULL;

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fms_app;
