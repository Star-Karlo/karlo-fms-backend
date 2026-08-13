-- Custom per-account roles with a granular permission set (RBAC).
--
-- The company admin creates roles and assigns each user one via users.role_id.
-- `permissions` holds keys from the app's permission catalog (feature.capability,
-- e.g. 'fuel.view', 'fuel.price_calc', 'vehicles.edit'). A role can only usefully
-- grant a permission whose feature the ACCOUNT has enabled (checked in the app).
--
-- Migration safety: users.role_id is NULLABLE. A user with no assigned role keeps
-- their legacy rank (users.role) so nobody is locked out when this ships; access
-- moves to the role's permissions once one is assigned.
CREATE TABLE roles (
  id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint      NOT NULL REFERENCES tenants(id),
  name        text        NOT NULL,
  permissions text[]      NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name)
);

-- Same tenant isolation every business table has.
ALTER TABLE roles ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON roles
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

CREATE INDEX idx_roles_tenant ON roles (tenant_id);

-- The custom role assigned to a user (NULL = fall back to the legacy rank).
ALTER TABLE users ADD COLUMN role_id bigint REFERENCES roles(id) ON DELETE SET NULL;
