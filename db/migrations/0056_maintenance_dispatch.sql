-- 0055_maintenance_dispatch.sql — send a vehicle to a hangar for its service.
--
-- The pieces already existed separately and had nothing joining them: a
-- service schedule knows a vehicle is due, a hangar knows where the workshop
-- is and can route to it, and site_visits already notices when a truck arrives
-- there. What was missing was the act of DISPATCHING — recording that this
-- schedule sends this truck to that workshop, and when.
--
-- Added to service_schedules rather than given its own table. A schedule can
-- be in service once at a time, so a separate dispatch table would hold at
-- most one open row per schedule and buy only the history — which the status
-- column plus an ordinary audit trail already covers well enough.

-- hangar_id already exists: 0026_hangars.sql added it as "which hangar a
-- service is booked at". What follows turns that from a note into an event.

ALTER TABLE service_schedules
  -- When it was sent. Distinct from last_done_at (when the work finished) and
  -- from next_due_at (when it became due): the gap between dispatch and
  -- arrival is the question "how long did it take the truck to get there",
  -- which is the one routing was added for.
  ADD COLUMN IF NOT EXISTS dispatched_at timestamptz,
  -- Who sent it. A truck being taken off the road is an operational decision
  -- and someone owns it.
  ADD COLUMN IF NOT EXISTS dispatched_by bigint REFERENCES users(id) ON DELETE SET NULL,
  -- The routed distance at dispatch, in km, as the engine answered it then.
  -- Stored rather than recomputed: the road network changes, and "how far was
  -- it when we sent it" must not drift afterwards.
  ADD COLUMN IF NOT EXISTS dispatch_distance_km double precision,
  ADD COLUMN IF NOT EXISTS dispatch_eta_min double precision;

-- The dashboard's main question is "what is in service, and where", so the
-- index covers the status it filters on rather than the whole table.
CREATE INDEX IF NOT EXISTS idx_service_schedules_in_service
  ON service_schedules (tenant_id, hangar_id)
  WHERE status = 'in_service';

-- A dispatched schedule must name its hangar. Without this a row could claim
-- to be in service with no workshop, which reads as data loss rather than as
-- the state it actually is.
ALTER TABLE service_schedules
  DROP CONSTRAINT IF EXISTS service_schedules_dispatch_has_hangar;
ALTER TABLE service_schedules
  ADD CONSTRAINT service_schedules_dispatch_has_hangar
  CHECK (dispatched_at IS NULL OR hangar_id IS NOT NULL);
