-- 0009_tracking_links.sql — shareable, time-limited public tracking links.
-- A link exposes a limited live view of selected vehicles to an external party
-- (via an unguessable token) without an account.

CREATE TABLE IF NOT EXISTS tracking_links (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  token       text NOT NULL UNIQUE,
  name        text NOT NULL,
  note        text,
  status      text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
  expires_at  timestamptz NOT NULL,
  show_fields jsonb NOT NULL DEFAULT '{}',   -- {driver:bool,last_seen:bool,speed:bool,temperature:bool,machine:bool}
  vehicle_ids jsonb NOT NULL DEFAULT '[]',   -- [vehicle_id,...]
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_tracking_links_tenant ON tracking_links (tenant_id);

CREATE TRIGGER trg_tracking_links_touch BEFORE UPDATE ON tracking_links
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Permissive-when-unset (like users): a public token lookup runs with no tenant
-- scope, then the handler sets the tenant from the resolved link.
ALTER TABLE tracking_links ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON tracking_links;
CREATE POLICY tenant_isolation ON tracking_links
  USING (NULLIF(current_setting('app.current_tenant', true), '') IS NULL
         OR tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (NULLIF(current_setting('app.current_tenant', true), '') IS NULL
         OR tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
