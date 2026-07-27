-- 0033_rule_window.sql — frequency (windowed) notification rules.
--
-- A camera event like DMS_SMOKING already lands as one alert per occurrence
-- (via the dashcam alarm poller). This lets an operator say "only notify me
-- when it keeps happening": fire when >= threshold events of alert_code occur
-- for a vehicle within window_minutes. NULL window keeps the old instantaneous
-- behaviour (e.g. OVERSPEED, or the per-event camera alerts themselves).
ALTER TABLE notification_rules ADD COLUMN IF NOT EXISTS window_minutes int;

COMMENT ON COLUMN notification_rules.window_minutes IS
  'Frequency rules: fire when >= threshold events of alert_code occur within this many minutes for a vehicle. NULL = instantaneous rule.';
