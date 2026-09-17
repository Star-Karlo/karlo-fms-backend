-- SOP run state: when each "out of procedure" condition was first observed
-- per vehicle (notifengine/sop.go). Was in-process only, so every deploy
-- forgot a run in progress and a 5-hour stop restarted at zero. Rewritten
-- each engine tick for the tenant; tiny.
CREATE TABLE IF NOT EXISTS sop_runs (
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  vehicle_id bigint NOT NULL,
  code       text   NOT NULL,
  since      timestamptz NOT NULL,
  PRIMARY KEY (tenant_id, vehicle_id, code)
);
