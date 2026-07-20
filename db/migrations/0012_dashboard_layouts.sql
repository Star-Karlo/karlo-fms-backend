-- 0012_dashboard_layouts.sql — per-user, drag-and-drop dashboard layouts.
-- One row per (tenant, user); `layout` holds the react-grid-layout items + widget
-- keys as JSON. Falls back to a role preset (frontend) when a user has no row.

CREATE TABLE IF NOT EXISTS dashboard_layouts (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  user_id    bigint NOT NULL REFERENCES users(id),
  layout     jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, user_id)
);

ALTER TABLE dashboard_layouts ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON dashboard_layouts;
CREATE POLICY tenant_isolation ON dashboard_layouts
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON dashboard_layouts TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
