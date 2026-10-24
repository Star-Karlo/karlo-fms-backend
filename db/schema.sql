-- schema.sql — sqlc-only view of the foundation tables. NOT applied to the DB
-- (db/migrations is the runtime source of truth). Keep the CREATE TABLE columns
-- here identical to the migrations; the RLS/role/trigger/stub machinery is
-- intentionally omitted because sqlc's parser only needs table shapes.

CREATE TABLE tenants (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name       text NOT NULL,
  slug       text NOT NULL UNIQUE,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE fleet_groups (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  name        text NOT NULL,
  description text,
  master_data_id text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  email         text NOT NULL UNIQUE,
  password_hash text NOT NULL,
  full_name     text,
  role          text NOT NULL DEFAULT 'operator',
  status        text NOT NULL DEFAULT 'active',
  is_root       boolean NOT NULL DEFAULT false,
  last_login_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  user_id     bigint NOT NULL REFERENCES users(id),
  token_hash  bytea NOT NULL UNIQUE,
  expires_at  timestamptz NOT NULL,
  revoked_at  timestamptz,
  replaced_by bigint REFERENCES refresh_tokens(id),
  user_agent  text,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE employees (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id      bigint NOT NULL REFERENCES tenants(id),
  employee_no    text,
  full_name      text NOT NULL,
  phone          text,
  role           text NOT NULL DEFAULT 'driver',
  license_no     text,
  license_class  text,
  license_expiry date,
  status         text NOT NULL DEFAULT 'active',
  external_ref   text,
  -- karlo_masterdata drivers._id. See migration 0043. Keep this file in step
  -- with db/migrations by hand — sqlc reads only this one, and 0042 forgetting
  -- to do so meant codegen failed on a column live in production.
  master_data_id text,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE trackers (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  imei        text NOT NULL,
  phone_no    text,
  vehicle_id  bigint,
  status      text NOT NULL DEFAULT 'active',
  external_ref text,
  master_data_id text,
  is_demo     boolean NOT NULL DEFAULT false,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE vehicles (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id         bigint NOT NULL REFERENCES tenants(id),
  license_plate     text NOT NULL,
  manufacturer      text,
  vehicle_type      text,
  box_type          text,
  unit_year         int,
  hull_no           text,
  fleet_group_id    bigint REFERENCES fleet_groups(id),
  status            text NOT NULL DEFAULT 'active',
  garage            text,
  odometer          numeric,
  hourmeter         numeric,
  fuel_tank_liters  double precision,
  fuel_ratio_kmpl   double precision,
  tracker_id        bigint REFERENCES trackers(id),
  current_driver_id bigint REFERENCES employees(id),
  notes             text,
  documents         jsonb NOT NULL DEFAULT '[]',
  external_ref      text,
  color             text,
  attributes        jsonb NOT NULL DEFAULT '{}',
  -- karlo_masterdata vehicles._id; the projection's stable join key. Added by
  -- migration 0042 — this file is sqlc's only view of the schema and has to be
  -- kept in step with db/migrations by hand, which 0042 did not do.
  master_data_id    text,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alerts (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id    bigint NOT NULL REFERENCES tenants(id),
  vehicle_id   bigint REFERENCES vehicles(id),
  tracker_id   bigint REFERENCES trackers(id),
  driver_id    bigint REFERENCES employees(id),
  source       text,
  device_source text,
  alert_code   text,
  alert_name   text,
  severity     text,
  occurred_at  timestamptz,
  actual_value numeric,
  limit_value  numeric,
  lat          double precision,
  lon          double precision,
  media_url    text,
  address      text,
  check_status text NOT NULL DEFAULT 'unchecked',
  note         text,
  read_at      timestamptz,
  checked_at   timestamptz,
  checked_by   bigint,
  payload      jsonb NOT NULL DEFAULT '{}',
  created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE notification_rules (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id         bigint NOT NULL REFERENCES tenants(id),
  rule_name         text NOT NULL,
  alert_code        text NOT NULL,
  notification_name text,
  urgency           text NOT NULL DEFAULT 'important',
  status            text NOT NULL DEFAULT 'active',
  applies_to        text NOT NULL DEFAULT 'all',
  target            jsonb NOT NULL DEFAULT '{}',
  threshold         numeric,
  window_minutes    int,
  sent_to_self      boolean NOT NULL DEFAULT false,
  geofence_scoped   boolean NOT NULL DEFAULT false,
  time_scoped       boolean NOT NULL DEFAULT false,
  auto_popup        boolean NOT NULL DEFAULT false,
  delivery          jsonb NOT NULL DEFAULT '{}',
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE dashboard_layouts (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  user_id    bigint NOT NULL REFERENCES users(id),
  layout     jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, user_id)
);

CREATE TABLE rule_vehicles (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  rule_id    bigint NOT NULL REFERENCES notification_rules(id) ON DELETE CASCADE,
  vehicle_id bigint NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, rule_id, vehicle_id)
);

CREATE TABLE documents (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  owner_type text NOT NULL,
  owner_id   bigint NOT NULL,
  kind       text,
  title      text,
  number     text,
  s3_key     text,
  mime       text,
  expiry     date,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE service_schedules (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id      bigint NOT NULL REFERENCES tenants(id),
  vehicle_id     bigint REFERENCES vehicles(id),
  component      text,
  scheduler      text,
  km_interval    numeric,
  km_reminder    numeric,
  day_interval   int,
  day_reminder   int,
  last_done_at   date,
  last_odometer  numeric,
  next_due_at    date,
  next_odometer  numeric,
  status         text NOT NULL DEFAULT 'normal',
  created_at     timestamptz NOT NULL DEFAULT now(),
  hangar_id     bigint,
  -- Dispatch: 0056_maintenance_dispatch.sql. Kept in sync by hand, because
  -- sqlc reads THIS file and not db/migrations — a column added only to a
  -- migration fails codegen with "column does not exist", which reads like a
  -- query bug rather than a sync one.
  dispatched_at        timestamptz,
  dispatched_by        bigint,
  dispatch_distance_km double precision,
  dispatch_eta_min     double precision
);

CREATE TABLE driver_score_events (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  driver_id   bigint REFERENCES employees(id),
  vehicle_id  bigint REFERENCES vehicles(id),
  event_type  text,
  occurrences int,
  score       numeric,
  period      date,
  metadata    jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tracking_links (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  token       text NOT NULL UNIQUE,
  name        text NOT NULL,
  note        text,
  status      text NOT NULL DEFAULT 'active',
  expires_at  timestamptz NOT NULL,
  show_fields jsonb NOT NULL DEFAULT '{}',
  vehicle_ids jsonb NOT NULL DEFAULT '[]',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE vehicle_telemetry_state (
  id                   bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id            bigint NOT NULL REFERENCES tenants(id),
  vehicle_id           bigint NOT NULL REFERENCES vehicles(id),
  engine_hours_seconds bigint NOT NULL DEFAULT 0,
  last_ignition        boolean,
  last_odometer_km     numeric,
  last_voltage_mv      numeric,
  last_gsm_signal      numeric,
  last_lat             double precision,
  last_lon             double precision,
  last_sample_at       timestamptz,
  inside_geofences     jsonb NOT NULL DEFAULT '[]',
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, vehicle_id)
);

CREATE TABLE api_tokens (
  id           bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id    bigint NOT NULL REFERENCES tenants(id),
  name         text NOT NULL,
  token_prefix text NOT NULL,
  token_hash   bytea NOT NULL,
  created_by   bigint REFERENCES users(id),
  last_used_at timestamptz,
  revoked_at   timestamptz,
  -- true: FMS issued this key and verifies it itself. false: the shared auth
  -- service owns it (see 0054).
  local_auth   boolean NOT NULL DEFAULT true,
  created_at   timestamptz NOT NULL DEFAULT now()
);

-- Maintenance hangars (0026). Trimmed: the generated geography column, RLS
-- policy and trigger live in db/migrations and are not needed for codegen.
CREATE TABLE hangars (
  id         bigserial PRIMARY KEY,
  tenant_id  bigint NOT NULL,
  name       text NOT NULL,
  address    text,
  phone      text,
  notes      text,
  lat        double precision NOT NULL,
  lon        double precision NOT NULL,
  radius_m   integer NOT NULL DEFAULT 150,
  capacity   integer,
  active     boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- Dashcams (VSS/MDVR devices) linked to vehicles. Trimmed for sqlc codegen;
-- the authoritative definition (RLS, indexes) is db/migrations/0028_dashcams.sql.
CREATE TABLE dashcams (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  vehicle_id    bigint REFERENCES vehicles(id) ON DELETE SET NULL,
  vss_device_id text NOT NULL,
  device_name   text,
  device_type   text,
  channels      int NOT NULL DEFAULT 0,
  channel_names text,
  access_mode   int,
  master_data_id text,
  is_demo       boolean NOT NULL DEFAULT false,
  created_at    timestamptz NOT NULL DEFAULT now()
);

-- Tracker↔vehicle assignment history (see 0040). Table only; the maintenance
-- trigger + RLS live in the migration and aren't needed for codegen.
CREATE TABLE tracker_assignments (
  id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint      NOT NULL REFERENCES tenants(id),
  tracker_id  bigint      NOT NULL REFERENCES trackers(id)  ON DELETE CASCADE,
  vehicle_id  bigint      NOT NULL REFERENCES vehicles(id)  ON DELETE CASCADE,
  imei        text        NOT NULL,
  fitted_at   timestamptz NOT NULL DEFAULT now(),
  unfitted_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE company_sop_settings (
  tenant_id            bigint PRIMARY KEY REFERENCES tenants(id),
  sop_enabled          boolean NOT NULL DEFAULT false,
  speed_limit_kmh      numeric NOT NULL DEFAULT 80,
  overspeed_min        int     NOT NULL DEFAULT 30,
  stop_max_min         int     NOT NULL DEFAULT 300,
  offline_min          int     NOT NULL DEFAULT 30,
  sensor_timeout_min   int     NOT NULL DEFAULT 60,
  idle_min             int     NOT NULL DEFAULT 15,
  fuel_price_per_litre numeric,
  fuel_type            text NOT NULL DEFAULT 'biosolar',
  idle_lph             numeric NOT NULL DEFAULT 2.5,
  updated_by           bigint,
  updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE fuel_prices (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  fuel_type      text    NOT NULL,
  price          numeric NOT NULL,
  effective_from date    NOT NULL,
  source         text,
  note           text,
  created_by     bigint,
  created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sop_runs (
  tenant_id  bigint NOT NULL,
  vehicle_id bigint NOT NULL,
  code       text   NOT NULL,
  since      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, vehicle_id, code)
);
