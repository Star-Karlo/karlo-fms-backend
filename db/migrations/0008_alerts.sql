-- 0008_alerts.sql — promote the alerts stub to a full table and add the
-- notification-rule engine. Feeds Control Tower, Notification, and the Vehicle
-- Alerts History report from one shared model.

-- Extend the alerts table (created as a stub in 0006).
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS driver_id     bigint REFERENCES employees(id);
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS device_source text;        -- 'camera' | 'gps'
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS alert_name    text;        -- human label
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS lat           double precision;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS lon           double precision;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS read_at       timestamptz;
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS checked_at    timestamptz;

CREATE INDEX IF NOT EXISTS idx_alerts_occurred ON alerts (tenant_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_alerts_status   ON alerts (tenant_id, check_status);
CREATE INDEX IF NOT EXISTS idx_alerts_code     ON alerts (tenant_id, alert_code);

-- Notification rules: what counts as an alert, thresholds, and who is told.
CREATE TABLE IF NOT EXISTS notification_rules (
  id                bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id         bigint NOT NULL REFERENCES tenants(id),
  rule_name         text NOT NULL,
  alert_code        text NOT NULL,                 -- from the alert taxonomy
  notification_name text,                          -- localized display name
  urgency           text NOT NULL DEFAULT 'important' CHECK (urgency IN ('important','urgent')),
  status            text NOT NULL DEFAULT 'active'   CHECK (status IN ('active','inactive')),
  applies_to        text NOT NULL DEFAULT 'all'      CHECK (applies_to IN ('all','group','selected')),
  target            jsonb NOT NULL DEFAULT '{}',    -- {group_ids:[], vehicle_ids:[]}
  threshold         numeric,                        -- e.g. overspeed km/h
  sent_to_self      boolean NOT NULL DEFAULT false,
  geofence_scoped   boolean NOT NULL DEFAULT false,
  time_scoped       boolean NOT NULL DEFAULT false,
  auto_popup        boolean NOT NULL DEFAULT false,
  delivery          jsonb NOT NULL DEFAULT '{}',    -- {in_platform:'all'|'users', recipients:[...]}
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_notification_rules_tenant ON notification_rules (tenant_id);
CREATE TRIGGER trg_notification_rules_touch BEFORE UPDATE ON notification_rules
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- RLS for notification_rules (strict tenant isolation).
ALTER TABLE notification_rules ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON notification_rules;
CREATE POLICY tenant_isolation ON notification_rules
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
