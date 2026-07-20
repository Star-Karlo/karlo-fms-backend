-- 0014_user_prefs.sql — per-user UI preferences (trip-view defaults, sensor
-- tile layout, etc.) as one jsonb blob per (tenant, user). Mirrors 0012.

CREATE TABLE IF NOT EXISTS user_prefs (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  user_id    bigint NOT NULL REFERENCES users(id),
  prefs      jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, user_id)
);

ALTER TABLE user_prefs ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON user_prefs;
CREATE POLICY tenant_isolation ON user_prefs
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON user_prefs TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
