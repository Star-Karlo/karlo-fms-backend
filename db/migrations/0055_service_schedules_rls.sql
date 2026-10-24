-- 0055_service_schedules_rls.sql — close a cross-tenant hole on service_schedules.
--
-- THE BUG. Eighteen tables in this schema carry the tenant_isolation policy;
-- service_schedules does not, and its queries carry no tenant filter either:
--
--   ListServiceSchedules   WHERE (q ...) AND (status ...)   -- no tenant_id
--   CountServiceSchedules  same
--   ServiceStatusCounts    no WHERE clause at all
--   DeleteServiceSchedule  WHERE id = $1                    -- any id
--
-- db.Tx sets app.current_tenant on every transaction, so the mechanism was
-- there and doing nothing: with no policy on the table, the setting is inert.
-- The result is that any tenant could read every tenant's maintenance plan,
-- and delete any row in it by guessing an id.
--
-- Fixed with the policy rather than by editing four queries, for two reasons:
-- it matches what every other table does, and it covers queries nobody has
-- written yet. A WHERE clause protects the query it is in; a policy protects
-- the table.
--
-- The table predates the convention (it arrived in 0006_stubs.sql, before RLS
-- was applied systematically) which is the likely reason it was skipped.

ALTER TABLE service_schedules ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON service_schedules;
CREATE POLICY tenant_isolation ON service_schedules
  USING      (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON service_schedules TO fms_app;

-- Reads that do not go through db.Tx get NOTHING rather than everything,
-- because the policy evaluates NULL = tenant_id as false. That is the right
-- failure direction: a report that silently returns no rows is noticed, a
-- report that silently returns another company's fleet is not.
