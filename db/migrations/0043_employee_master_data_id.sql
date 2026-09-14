-- 0043_employee_master_data_id.sql — the join key for the driver projection.
--
-- The same seam 0042 opened for vehicles, now for drivers, and for a reason
-- that turned out to be more urgent than the vehicle one.
--
-- WHAT WENT WRONG WITHOUT IT.
--
-- After the migration, drivers live in karlo_masterdata and FMS's screens
-- create them there directly. But fms_app.vehicles.current_driver_id is a
-- BIGINT referencing employees(id), and nothing was creating the local row. So
-- a driver created after the cutover existed in master data with no employees
-- row, no bigint, and therefore could not be attached to a vehicle at all.
--
-- The vehicle form labelled those drivers "added after the migration, not yet
-- assignable" and greyed them out, which was honest but not a solution: for a
-- company onboarded AFTER the migration every driver is in that state, so the
-- Current Driver field could never be set. DEMO ACCOUNT hit exactly that — three
-- drivers, all unassignable, on a brand-new company with no other drivers to
-- pick.
--
-- The migration stamped attributes.fmsEmployeeId onto the 1,500 drivers it
-- moved, and that is what "assignable" was reading. Nothing wrote it afterwards
-- and nothing ever would, so the state was permanent rather than transitional.
--
-- WHY A COLUMN AND NOT A LOOKUP ON attributes.fmsEmployeeId.
--
-- That attribute lives upstream and points this way; this column lives here and
-- points upstream. Both exist because each side needs to resolve the other
-- without asking, and a join through a remote JSON attribute is not something
-- sixteen local queries can do inside a transaction.
--
-- It is also the key driversync matches on, exactly as vehiclesync does for
-- vehicles — the ObjectId is stable where a name is not. Two drivers can share
-- a full name; people also change them.

ALTER TABLE employees
    ADD COLUMN IF NOT EXISTS master_data_id TEXT;

COMMENT ON COLUMN employees.master_data_id IS
    'karlo_masterdata drivers._id. Set when FMS creates a driver through the '
    'write-through path, and by driversync for drivers created elsewhere. NULL '
    'means this row has not been reconciled with master data yet.';

-- Partial, matching 0042: NULL is the normal state for a row that predates the
-- pairing, and there is no uniqueness worth enforcing among unreconciled rows.
CREATE UNIQUE INDEX IF NOT EXISTS idx_employees_master_data_id
    ON employees (master_data_id) WHERE master_data_id IS NOT NULL;
