-- 0004_vehicles.sql — trackers (GPS devices) and vehicles (the assets).
--
-- tracker.imei is the JOIN KEY to the telemetry pipeline: fms-backend resolves a
-- vehicle's IMEI, then calls the telemetry API (fms-tracking) by IMEI to fetch
-- live/historical position. There is no SQL-level FK to telemetry — the two
-- databases are joined over HTTP.
--
-- vehicles.tracker_id <-> trackers.vehicle_id is a mutual (chicken-and-egg) FK
-- pair; both nullable, and trackers.vehicle_id's FK is added after vehicles
-- exists. A direct link is sufficient for the foundation; a temporal
-- device<->vehicle assignment history (for box-swapping) is a deliberate future
-- migration, not built now.

CREATE TABLE trackers (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  imei        text   NOT NULL,                          -- telemetry join key
  phone_no    text,
  vehicle_id  bigint,                                   -- FK added below
  status      text   NOT NULL DEFAULT 'active'
              CHECK (status IN ('active','inactive')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, imei)
);
CREATE INDEX idx_trackers_tenant ON trackers (tenant_id);
CREATE INDEX idx_trackers_imei   ON trackers (imei);
CREATE TRIGGER trg_trackers_touch BEFORE UPDATE ON trackers
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

CREATE TABLE vehicles (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id         bigint NOT NULL REFERENCES tenants(id),
  license_plate     text   NOT NULL,
  manufacturer      text,
  vehicle_type      text,                               -- truck | dump_truck | van ...
  box_type          text,
  unit_year         int,
  hull_no           text,                               -- nomor lambung
  fleet_group_id    bigint REFERENCES fleet_groups(id),
  status            text   NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active','repair','archive')),
  garage            text,
  odometer          numeric,
  hourmeter         numeric,
  tracker_id        bigint REFERENCES trackers(id),
  current_driver_id bigint REFERENCES employees(id),
  notes             text,
  documents         jsonb NOT NULL DEFAULT '[]',        -- lightweight doc refs until Document module
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, license_plate)
);
CREATE INDEX idx_vehicles_tenant      ON vehicles (tenant_id);
CREATE INDEX idx_vehicles_fleet_group ON vehicles (fleet_group_id);
CREATE INDEX idx_vehicles_tracker     ON vehicles (tracker_id);
CREATE TRIGGER trg_vehicles_touch BEFORE UPDATE ON vehicles
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Complete the mutual link now that vehicles exists.
ALTER TABLE trackers
  ADD CONSTRAINT fk_trackers_vehicle
  FOREIGN KEY (vehicle_id) REFERENCES vehicles(id);

-- At most one active tracker bound to a given vehicle.
CREATE UNIQUE INDEX uq_tracker_vehicle_active
  ON trackers (vehicle_id)
  WHERE vehicle_id IS NOT NULL AND status = 'active';
