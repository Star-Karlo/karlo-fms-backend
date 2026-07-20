-- 0011_rule_vehicles.sql — attach notification rules to specific vehicles
-- (many-to-many). The evaluation engine checks each active rule only against
-- the vehicles it is attached to.

CREATE TABLE IF NOT EXISTS rule_vehicles (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id  bigint NOT NULL REFERENCES tenants(id),
  rule_id    bigint NOT NULL REFERENCES notification_rules(id) ON DELETE CASCADE,
  vehicle_id bigint NOT NULL REFERENCES vehicles(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, rule_id, vehicle_id)
);
CREATE INDEX IF NOT EXISTS idx_rule_vehicles_rule    ON rule_vehicles (tenant_id, rule_id);
CREATE INDEX IF NOT EXISTS idx_rule_vehicles_vehicle ON rule_vehicles (tenant_id, vehicle_id);

-- Dedup lookup for the engine: recent alert per (vehicle, code).
CREATE INDEX IF NOT EXISTS idx_alerts_vehicle_code_time ON alerts (tenant_id, vehicle_id, alert_code, occurred_at DESC);

ALTER TABLE rule_vehicles ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON rule_vehicles;
CREATE POLICY tenant_isolation ON rule_vehicles
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON rule_vehicles TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
