-- 0035_backfill_alert_device_source.sql — one-time.
--
-- Camera alarms were written with the default device_source 'gps' (CreateAlert
-- hardcoded it), so filtering alerts by device_source = 'camera' — which the
-- Recordings tab does to list a camera's events — returned nothing. Re-tag the
-- clearly camera-sourced alerts (DMS/ADAS events, the generic dashcam event, and
-- their frequency rollups, which share the DMS_/ADAS_ prefix) as 'camera'.
UPDATE alerts
SET device_source = 'camera'
WHERE device_source IS DISTINCT FROM 'camera'
  AND (starts_with(alert_code, 'DMS_')
       OR starts_with(alert_code, 'ADAS_')
       OR alert_code = 'DASHCAM_EVENT');
