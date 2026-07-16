-- 0001_core.sql — extensions, tenancy root, fleet groups, shared trigger.
--
-- This is the FMS *application* database (fms_app), separate from the telemetry
-- pipeline DB. It is low-volume relational CRUD: no hypertables here, so RLS can
-- apply uniformly (see 0005_rls.sql).

CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS btree_gist;
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- crypt()/gen_salt() for seeding bcrypt hashes

-- Shared trigger to keep updated_at current on any table that has the column.
CREATE OR REPLACE FUNCTION touch_updated_at() RETURNS trigger AS $$
BEGIN
  NEW.updated_at = now();
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- ---------------------------------------------------------------------------
-- Tenants — SaaS multi-tenancy root. Every business table carries tenant_id.
-- Currently the system runs with a single seeded tenant; the column + RLS are
-- in place so multi-tenancy is a config change, not a migration.
-- ---------------------------------------------------------------------------
CREATE TABLE tenants (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name       text NOT NULL,
  slug       text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

-- ---------------------------------------------------------------------------
-- Fleet groups — logical grouping of vehicles (e.g. by depot or contract).
-- ---------------------------------------------------------------------------
CREATE TABLE fleet_groups (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  name        text NOT NULL,
  description text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, name)
);
CREATE INDEX idx_fleet_groups_tenant ON fleet_groups (tenant_id);
CREATE TRIGGER trg_fleet_groups_touch BEFORE UPDATE ON fleet_groups
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
