-- Per-tenant feature entitlements (module-level).
--
-- Absence of a row = ENABLED. This makes the default opt-out: every existing
-- customer keeps all features until a platform admin explicitly turns one OFF,
-- so nothing breaks on rollout. Like `tenants`, this table carries NO RLS —
-- platform admins manage entitlements across tenants directly through the pool.
CREATE TABLE tenant_features (
  tenant_id   bigint      NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  feature_key text        NOT NULL,
  enabled     boolean     NOT NULL,
  updated_at  timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, feature_key)
);
