-- 0042_vehicle_master_data_id.sql — the join key for the fleet projection.
--
-- After the migration, karlo_masterdata owns the fleet register: vehicles are
-- created and edited there, by the FMS web app and by TMS alike. But sixteen
-- queries in this service still join `vehicles` to put a plate next to an alert,
-- a service schedule, a driver-score event or a document — and they join on the
-- BIGINT, because alerts.vehicle_id, driving_events.vehicle_id and every RLS
-- policy are bigint-keyed and always will be.
--
-- So `vehicles` becomes a PROJECTION, the same shape tenantsync already gives
-- `tenants`: master data is authoritative, a background loop keeps the local
-- copy in step, and the sixteen joins carry on untouched. Rewriting them to call
-- a service would put a network dependency inside transactions that currently
-- cannot fail that way.
--
-- WHY A NEW COLUMN RATHER THAN MATCHING ON THE PLATE EVERY TIME.
--
-- The first sync has to match on something the two systems share, and that is
-- the normalised plate — every migrated vehicle carries attributes.fmsVehicleId
-- in master data, but the gRPC Truck message does not expose it, and a plate is
-- unique per company on both sides.
--
-- A plate is not a stable identity, though. Trucks get re-plated: this very
-- dataset has six that were, which is why fix_duplicate_vehicles.sql had to
-- match on the chassis as well. Re-plate a truck upstream and a plate-keyed sync
-- sees a vehicle it does not recognise, inserts a second row, and the old one
-- keeps every alert and telemetry row ever recorded against it. Two trucks, one
-- lorry, history split down the middle and nothing reporting a problem.
--
-- The ObjectId does not change. Matched once by plate, recorded here, and every
-- later sync keys on it — so a re-plate is an UPDATE to license_plate, which is
-- what it actually is.

ALTER TABLE vehicles
    ADD COLUMN IF NOT EXISTS master_data_id TEXT;

COMMENT ON COLUMN vehicles.master_data_id IS
    'karlo_masterdata vehicles._id. Set by vehiclesync; the stable key the '
    'projection matches on. NULL means this row has not been reconciled with '
    'master data yet — on a deployment that has not cut over, that is every row.';

-- Partial, because NULL is the normal state before the first sync and there is
-- no meaningful uniqueness among rows that have not been reconciled.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vehicles_master_data_id
    ON vehicles (master_data_id) WHERE master_data_id IS NOT NULL;

-- Deliberately NOT scoped by tenant. A master-data vehicle belongs to exactly
-- one company, so the same ObjectId appearing under two FMS tenants means the
-- company map is wrong — and a global index turns that into a failed sync with
-- a clear message, instead of one truck quietly existing in two customers'
-- fleets. That is the same reasoning behind the global chassis index upstream.
