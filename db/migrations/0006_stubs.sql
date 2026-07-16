-- 0006_stubs.sql — schema anchors for future modules (no endpoints yet).
--
-- These tables are created now so later phases (documents, maintenance, alerts,
-- scorecard, geofences) have a stable schema to build on. They are tenant-scoped
-- and RLS-enabled like the rest, but no handlers read/write them in the
-- foundation slice.

-- Uploaded documents/attachments (vehicle STNK, driver SIM, insurance ...).
-- Will eventually replace the vehicles.documents jsonb.
CREATE TABLE documents (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  owner_type text NOT NULL,                     -- 'vehicle' | 'driver'
  owner_id   bigint NOT NULL,
  kind       text,                              -- STNK | SIM | insurance ...
  title      text,
  number     text,
  s3_key     text,
  mime       text,
  expiry     date,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_documents_tenant ON documents (tenant_id);
CREATE INDEX idx_documents_owner  ON documents (owner_type, owner_id);

-- Driver licenses / permits (License module).
CREATE TABLE licenses (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  employee_id   bigint REFERENCES employees(id),
  kind          text,
  number        text,
  class         text,
  issued_at     date,
  expiry        date,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_licenses_tenant ON licenses (tenant_id);

-- Scheduled maintenance (Fleet Management > Maintenance).
CREATE TABLE service_schedules (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id      bigint NOT NULL REFERENCES tenants(id),
  vehicle_id     bigint REFERENCES vehicles(id),
  component      text,
  scheduler      text,                          -- floating | fixed
  km_interval    numeric,
  km_reminder    numeric,
  day_interval   int,
  day_reminder   int,
  last_done_at   date,
  last_odometer  numeric,
  next_due_at    date,
  next_odometer  numeric,
  status         text NOT NULL DEFAULT 'normal',-- normal | due_soon | overdue | in_service
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_service_schedules_tenant  ON service_schedules (tenant_id);
CREATE INDEX idx_service_schedules_vehicle ON service_schedules (vehicle_id);

-- ADAS/DMS + system alerts (Notification Center / Control Tower).
CREATE TABLE alerts (
  id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id       bigint NOT NULL REFERENCES tenants(id),
  vehicle_id      bigint REFERENCES vehicles(id),
  tracker_id      bigint REFERENCES trackers(id),
  source          text,                         -- adas | dms | system | geofence
  alert_code      text,                         -- DMS_SEATBELT | ADAS_HMW ...
  severity        text,                         -- urgent | important | info
  occurred_at     timestamptz,
  actual_value    numeric,
  limit_value     numeric,
  media_url       text,
  address         text,
  check_status    text NOT NULL DEFAULT 'unchecked', -- unchecked | checked | false_alert
  note            text,
  payload         jsonb NOT NULL DEFAULT '{}',
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_alerts_tenant  ON alerts (tenant_id);
CREATE INDEX idx_alerts_vehicle ON alerts (vehicle_id);

-- Driver behavior scoring events (Driver Scorecard).
CREATE TABLE driver_score_events (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  driver_id   bigint REFERENCES employees(id),
  vehicle_id  bigint REFERENCES vehicles(id),
  event_type  text,                             -- sudden_accel | sharp_turn | yawning ...
  occurrences int,
  score       numeric,
  period      date,
  metadata    jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_driver_score_events_tenant ON driver_score_events (tenant_id);
CREATE INDEX idx_driver_score_events_driver ON driver_score_events (driver_id, period);

-- Geofence definitions owned by the app; radius/point-in-polygon evaluation is
-- delegated to the telemetry service's /v1/geofence/check.
CREATE TABLE geofences (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  name       text NOT NULL,
  geom       geography(Polygon, 4326),
  center     geography(Point, 4326),
  radius_m   numeric,
  rules      jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_geofences_tenant ON geofences (tenant_id);
CREATE INDEX idx_geofences_geom   ON geofences USING gist (geom);

-- RLS (strict) for all stub tables. NULLIF maps unset/'' to NULL (see 0005).
DO $$
DECLARE t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'documents','licenses','service_schedules','alerts','driver_score_events','geofences'
  ] LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
    EXECUTE format('DROP POLICY IF EXISTS tenant_isolation ON %I', t);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I '
      || 'USING (tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint) '
      || 'WITH CHECK (tenant_id = NULLIF(current_setting(''app.current_tenant'', true), '''')::bigint)',
      t);
  END LOOP;
END $$;

-- Ensure fms_app can use the tables created in this migration too.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
