-- 0026_hangars.sql — maintenance hangars: where vehicles get serviced.
--
-- WHY NOT A GEOFENCE. `geofences` already stores a named circle per tenant and
-- the collector already computes enter/exit for it, so a hangar could have been
-- one more row there. It is a separate table because the two answer different
-- questions and would fight over the same fields:
--
--   * a geofence exists to RAISE ALERTS — its rules jsonb drives notification
--     rules, and every geofence a tenant adds is another thing generating
--     events. A hangar that alerted on every arrival would be noise; trucks are
--     supposed to go there.
--   * a hangar is a PLACE WORK HAPPENS. It has a capacity, a phone number and
--     opening hours, and service schedules point at it. None of that belongs on
--     an alert zone.
--
-- Anyone who does want arrival alerts can still draw a geofence over the same
-- spot; the two are deliberately independent.

CREATE TABLE IF NOT EXISTS hangars (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  name       text   NOT NULL,
  address    text,
  phone      text,
  notes      text,

  lat        double precision NOT NULL CHECK (lat BETWEEN -11 AND 6),
  lon        double precision NOT NULL CHECK (lon BETWEEN 95 AND 141),

  -- Generated so it can never drift from lat/lon. Geography, not geometry, so
  -- ST_DWithin takes metres directly — which is the whole point here, since
  -- "is this truck at the hangar" is a distance question asked constantly.
  geog geography(Point, 4326)
    GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(lon, lat), 4326)::geography) STORED,

  -- How close counts as "at the hangar". Default 150m rather than something
  -- tighter because GPS in a metal-roofed workshop drifts badly, and a truck
  -- parked behind the building should still read as present.
  radius_m   integer NOT NULL DEFAULT 150 CHECK (radius_m BETWEEN 25 AND 5000),

  -- Capacity is advisory: it drives a "full" badge, not a constraint. A
  -- workshop that squeezes in one more vehicle should not fail a booking.
  capacity   integer CHECK (capacity IS NULL OR capacity > 0),

  active     boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_hangars_tenant ON hangars (tenant_id);
CREATE INDEX IF NOT EXISTS idx_hangars_geog   ON hangars USING GIST (geog);

-- Which hangar a service is booked at. Nullable: plenty of work is done
-- roadside or at a third party, and forcing a hangar would make the field a
-- lie rather than a fact.
ALTER TABLE service_schedules
  ADD COLUMN IF NOT EXISTS hangar_id bigint REFERENCES hangars(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_service_schedules_hangar ON service_schedules (hangar_id);

-- Tenant isolation, same shape as every other business table (see 0005). The
-- NULLIF dance maps both the never-set (NULL) and reset ('') cases to NULL so
-- ''::bigint never raises on a pooled connection.
ALTER TABLE hangars ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON hangars;
CREATE POLICY tenant_isolation ON hangars
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON hangars TO fms_app;

-- DROP-then-CREATE because PostgreSQL has no CREATE TRIGGER IF NOT EXISTS.
-- Every other statement in this file is guarded, so without this the migration
-- succeeds once and then fails on re-run — which is exactly the shape of
-- failure that makes a half-applied schema hard to reason about.
DROP TRIGGER IF EXISTS trg_hangars_touch ON hangars;
CREATE TRIGGER trg_hangars_touch BEFORE UPDATE ON hangars
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();
