-- 0005_rls.sql — tenant isolation via Row-Level Security.
--
-- The API connects as the non-owner role `fms_app` and runs
-- `set_config('app.current_tenant', <id>, true)` per request-transaction (see
-- internal/platform/db/db.go). Every query is then scoped to that tenant.
--
-- IMPORTANT: once a custom GUC has been SET LOCAL on a pooled connection, it
-- reverts to an EMPTY STRING (not NULL) after the transaction ends — and
-- ''::bigint raises. So every policy funnels the setting through
-- NULLIF(current_setting('app.current_tenant', true), '') which maps both the
-- never-set (NULL) and reset ('') cases to NULL, and only casts when non-empty.
--
-- Two policy shapes:
--   * STRICT (business tables): rows visible only when the tenant is set AND
--     matches tenant_id. Unset ⇒ no rows.
--   * LOGIN-BOOTSTRAP (users, refresh_tokens): also visible when the tenant is
--     unset, so login can resolve a user by (globally-unique) email before any
--     tenant scope exists.

-- Re-runnable: drop existing policies first.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'fleet_groups','employees','trackers','vehicles','users','refresh_tokens'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', t);
  END LOOP;
END $$;

-- Strict tenant tables.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['fleet_groups','employees','trackers','vehicles'] LOOP
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I '
      || 'USING (tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint) '
      || 'WITH CHECK (tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint)',
      t);
  END LOOP;
END $$;

-- Login-bootstrap tables: permissive when the tenant is unset.
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY['users','refresh_tokens'] LOOP
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I '
      || 'USING (NULLIF(current_setting(''app.current_tenant'', true), '''') IS NULL '
      || '       OR tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint) '
      || 'WITH CHECK (NULLIF(current_setting(''app.current_tenant'', true), '''') IS NULL '
      || '       OR tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint)',
      t);
  END LOOP;
END $$;

-- Non-owner app role, subject to RLS.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'fms_app') THEN
    CREATE ROLE fms_app LOGIN PASSWORD 'fms_app';
  END IF;
END $$;

GRANT USAGE ON SCHEMA public TO fms_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO fms_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO fms_app;
