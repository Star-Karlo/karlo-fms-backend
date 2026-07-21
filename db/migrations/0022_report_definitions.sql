-- 0022_report_definitions.sql — user-defined reports.
--
-- A custom report is a NAMED SPEC, not a stored result: which sections to
-- include, what to pass each one, and over what window. Running it re-runs the
-- underlying reports, so a report saved as "last 7 days" means the last 7 days
-- whenever it is opened, not the week it was created.
--
-- Sections reference the EXISTING catalog keys rather than inventing a query
-- language. That is the whole design: the 17 reports already work, are already
-- tenant-scoped, and already return {columns, rows} — a custom report is a
-- named combination of them plus presentation. Nothing here can express a
-- query the user could not already run.
--
-- Per (tenant, user): these are personal, like dashboard_layouts. Sharing is a
-- later decision and adding an owner column now would prejudge it.
--
-- Idempotent: scripts/migrate.sh re-applies every file on every deploy.

CREATE TABLE IF NOT EXISTS report_definitions (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint      NOT NULL REFERENCES tenants(id),
  user_id     bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        text        NOT NULL,
  description text        NOT NULL DEFAULT '',
  spec        jsonb       NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_report_definitions_owner
  ON report_definitions (tenant_id, user_id, updated_at DESC);

-- Same name twice in one account is a mistake, not a feature.
CREATE UNIQUE INDEX IF NOT EXISTS idx_report_definitions_name
  ON report_definitions (tenant_id, user_id, lower(name));

-- Tenancy is enforced by RLS, not by WHERE clauses in Go: the app connects as a
-- non-owner role, so even a bare SELECT is tenant-filtered. NULLIF guards the
-- unset case — an empty GUC would otherwise cast-error rather than match zero
-- rows.
ALTER TABLE report_definitions ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON report_definitions;
CREATE POLICY tenant_isolation ON report_definitions
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON report_definitions TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
