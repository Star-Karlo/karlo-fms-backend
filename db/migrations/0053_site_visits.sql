-- 0053_site_visits.sql — how long a vehicle has been at a place, and when it arrived.
--
-- Until now "who is at this site" was answered by comparing the latest position
-- against a radius: a boolean, recomputed on every request, with no memory. It
-- cannot say since when, cannot say for how long, and cannot be reported on
-- after the fact. The geofence report worked around that by parsing enter/exit
-- ALERT TEXT ("Entered Gudang Semarang") and pairing rows by the name it found
-- — which breaks the moment a geofence is renamed, and records nothing at all
-- for a vehicle with no notification rule attached.
--
-- A visit is the missing noun. One row per (vehicle, site, stay): opened when
-- the vehicle arrives, closed when it leaves, carrying the two timestamps that
-- every question above is really asking for.
--
-- ONE TABLE FOR BOTH SITE KINDS. A hangar and a geofence are the same shape —
-- a named circle a truck is inside or outside — and the queries over them are
-- identical. Two tables would mean two of every query and a presence endpoint
-- written twice. Separate nullable foreign keys rather than (kind, id) so each
-- still cascades when its site is deleted, with a CHECK that exactly one is set.

CREATE TABLE site_visits (
  id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint      NOT NULL REFERENCES tenants(id),
  geofence_id bigint      REFERENCES geofences(id) ON DELETE CASCADE,
  hangar_id   bigint      REFERENCES hangars(id)   ON DELETE CASCADE,
  vehicle_id  bigint      NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,

  -- First fix inside the radius. The stay is measured from here, including the
  -- part before it was confirmed: the truck was there, whatever the debounce
  -- had decided yet.
  entered_at  timestamptz NOT NULL,
  -- When the stay became long enough to count as a visit (geofences.min_dwell_min).
  -- NULL means still provisional — a truck driving straight through opens a row
  -- and closes it without ever being confirmed, and nothing downstream sees it.
  confirmed_at timestamptz,
  -- NULL = still there. Set from the last fix inside, not the first fix outside:
  -- a vehicle reporting every two minutes would otherwise gain two minutes of
  -- dwell for standing on the far side of the fence.
  exited_at   timestamptz,
  dwell_min   numeric,

  -- Last fix seen inside, so a stay can be closed by a later pass even if the
  -- vehicle went silent rather than leaving.
  last_seen_at timestamptz NOT NULL,
  -- An overstay alert fires once per visit, not once per collector pass.
  dwell_alerted_at timestamptz,

  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT site_visits_one_site CHECK ((geofence_id IS NULL) <> (hangar_id IS NULL))
);

-- At most one OPEN visit per vehicle per site. The collector relies on this:
-- it upserts the open row rather than reading-then-writing, so two passes
-- racing cannot produce two overlapping stays.
CREATE UNIQUE INDEX site_visits_open_geofence_uk ON site_visits (geofence_id, vehicle_id)
  WHERE exited_at IS NULL AND geofence_id IS NOT NULL;
CREATE UNIQUE INDEX site_visits_open_hangar_uk ON site_visits (hangar_id, vehicle_id)
  WHERE exited_at IS NULL AND hangar_id IS NOT NULL;

-- "Who is here now", the presence panel.
CREATE INDEX site_visits_open ON site_visits (tenant_id, geofence_id, hangar_id) WHERE exited_at IS NULL;
-- "What happened at these sites between these dates", the report, ordered by time.
CREATE INDEX site_visits_window ON site_visits (tenant_id, entered_at DESC);
-- One vehicle's own history of visits.
CREATE INDEX site_visits_vehicle ON site_visits (tenant_id, vehicle_id, entered_at DESC);

ALTER TABLE site_visits ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON site_visits;
CREATE POLICY tenant_isolation ON site_visits
  USING      (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);
GRANT SELECT, INSERT, UPDATE, DELETE ON site_visits TO fms_app;

-- Geofences grow the fields a hangar already has, so the two can be managed by
-- the same kind of page instead of a cramped panel on the map.
ALTER TABLE geofences
  ADD COLUMN IF NOT EXISTS address       text,
  ADD COLUMN IF NOT EXISTS notes         text,
  ADD COLUMN IF NOT EXISTS active        boolean     NOT NULL DEFAULT true,
  -- How long inside before it counts. A truck on a road that clips the circle
  -- is not a visit, and alerting on it trains people to ignore alerts; 0 keeps
  -- the old behaviour (the first fix inside counts).
  ADD COLUMN IF NOT EXISTS min_dwell_min integer     NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS updated_at    timestamptz NOT NULL DEFAULT now();

-- Geofence membership is now an OPT-IN narrowing, not a prerequisite. A
-- geofence with no rows in geofence_vehicles watches the whole fleet, which is
-- what people expect of a site they just drew on the map; attaching vehicles
-- narrows it to those. Without this, a new geofence silently watches nothing.
COMMENT ON TABLE geofence_vehicles IS
  'Optional narrowing: when a geofence has no rows here it applies to every vehicle in the account.';
