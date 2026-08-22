-- 0040_tracker_assignments.sql — a tracker↔vehicle assignment HISTORY.
--
-- The bug this fixes: the tracker↔vehicle link was current-only (trackers.vehicle_id
-- overwritten on reassign). Telemetry is keyed by IMEI, so once a device moves to
-- another truck, every historical read against the CURRENT vehicle silently credits
-- the earlier truck's distance / fuel-per-km / driver score to the new one — all-real
-- positions, wrong vehicle, no error. This records each period a device was fitted to
-- a vehicle, so history resolves to whichever truck actually carried it at that time.

CREATE TABLE tracker_assignments (
  id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint      NOT NULL REFERENCES tenants(id),
  tracker_id  bigint      NOT NULL REFERENCES trackers(id)  ON DELETE CASCADE,
  vehicle_id  bigint      NOT NULL REFERENCES vehicles(id)  ON DELETE CASCADE,
  -- Denormalised so the time-resolution query (VehicleAt by IMEI) needs no join
  -- and survives even if the tracker row is later edited.
  imei        text        NOT NULL,
  fitted_at   timestamptz NOT NULL DEFAULT now(),
  unfitted_at timestamptz,                                  -- NULL = currently fitted
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- At most one OPEN assignment per tracker (a device is on one vehicle at a time).
CREATE UNIQUE INDEX tracker_assignments_open_uk ON tracker_assignments (tracker_id) WHERE unfitted_at IS NULL;
-- VehicleAt(imei, t): newest assignment covering t.
CREATE INDEX tracker_assignments_imei_time ON tracker_assignments (tenant_id, imei, fitted_at DESC);
-- A vehicle's device periods within a window.
CREATE INDEX tracker_assignments_vehicle ON tracker_assignments (tenant_id, vehicle_id, fitted_at DESC);

-- RLS, same strict tenant isolation as the rest of the schema.
ALTER TABLE tracker_assignments ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON tracker_assignments;
CREATE POLICY tenant_isolation ON tracker_assignments
  USING      (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);
GRANT SELECT, INSERT, UPDATE, DELETE ON tracker_assignments TO fms_app;

-- Backfill: the current link becomes one open assignment, started at the tracker's
-- creation (best-effort — we have no earlier history). Everything before that resolves
-- to the current vehicle via the fallback in code; moves from here on are recorded.
INSERT INTO tracker_assignments (tenant_id, tracker_id, vehicle_id, imei, fitted_at, unfitted_at)
SELECT tenant_id, id, vehicle_id, imei, COALESCE(created_at, now()), NULL
FROM trackers WHERE vehicle_id IS NOT NULL;

-- Maintenance by TRIGGER on trackers.vehicle_id, so no application path can forget:
-- AssignTracker (bind) and the CSV importer both write trackers.vehicle_id, which is
-- where real device MOVES happen (the source of the mis-attribution). The trigger runs
-- as the caller — under the app that's fms_app with app.current_tenant set (NEW.tenant_id
-- matches, so the WITH CHECK passes); the importer runs as owner (RLS bypassed).
CREATE OR REPLACE FUNCTION sync_tracker_assignment() RETURNS trigger AS $$
BEGIN
  -- Close the tracker's open assignment when it leaves a vehicle (moved or unfitted).
  IF TG_OP = 'UPDATE' AND NEW.vehicle_id IS DISTINCT FROM OLD.vehicle_id THEN
    UPDATE tracker_assignments SET unfitted_at = now()
      WHERE tracker_id = NEW.id AND unfitted_at IS NULL;
  END IF;
  -- Open a new assignment when it is (newly) fitted to a vehicle.
  IF NEW.vehicle_id IS NOT NULL AND (TG_OP = 'INSERT' OR NEW.vehicle_id IS DISTINCT FROM OLD.vehicle_id) THEN
    INSERT INTO tracker_assignments (tenant_id, tracker_id, vehicle_id, imei, fitted_at)
      VALUES (NEW.tenant_id, NEW.id, NEW.vehicle_id, NEW.imei, now());
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_sync_tracker_assignment
  AFTER INSERT OR UPDATE OF vehicle_id ON trackers
  FOR EACH ROW EXECUTE FUNCTION sync_tracker_assignment();
