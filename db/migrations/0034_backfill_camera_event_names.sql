-- 0034_backfill_camera_event_names.sql — one-time.
--
-- Camera alerts created before codes.go named the Howen AOE codes read
-- "Camera event (code N)" with alert_code DASHCAM_EVENT and a flat 'important'
-- severity. The numeric code is embedded in the name, so re-derive the proper
-- code / name / severity from it, using the same mapping as codes.go. Codes not
-- in the table are left untouched (we don't rename what we can't identify).
--
-- Runs as the migration owner, so it updates every tenant's rows (RLS is not in
-- effect for the owner). Idempotent: once a row's alert_code is no longer
-- DASHCAM_EVENT it no longer matches.

WITH codemap(code, alert_code, name, severity) AS (
  VALUES
    -- ADAS (road-facing lens, 11x)
    (110, 'ADAS_FCW',         'Forward collision warning (ADAS)',        'urgent'),
    (111, 'ADAS_LDW',         'Lane departure (ADAS)',                   'important'),
    (112, 'ADAS_HMW',         'Following too close (ADAS)',              'important'),
    (113, 'ADAS_PCW',         'Pedestrian collision warning (ADAS)',     'urgent'),
    (114, 'ADAS_LANE_CHANGE', 'Frequent lane change (ADAS)',            'important'),
    (115, 'ADAS_ROAD_SIGN',   'Road-sign / speed-sign alert (ADAS)',    'important'),
    (116, 'ADAS_OBSTACLE',    'Obstacle / blind spot (ADAS)',           'important'),
    (117, 'ADAS_EVENT',       'Road-safety event (ADAS)',               'important'),
    -- DMS (driver-facing lens, 12x)
    (120, 'DMS_NO_DRIVER',      'Driver absent (DMS)',                  'urgent'),
    (121, 'DMS_FATIGUE',        'Driver fatigue / drowsiness (DMS)',    'urgent'),
    (122, 'DMS_PHONE',          'Phone use while driving (DMS)',        'urgent'),
    (123, 'DMS_SMOKING',        'Smoking while driving (DMS)',          'important'),
    (124, 'DMS_DISTRACTION',    'Driver distraction (DMS)',             'urgent'),
    (125, 'DMS_CAMERA_BLOCKED', 'Driver camera obstructed (DMS)',       'important'),
    (126, 'DMS_YAWN',           'Yawning (DMS)',                        'important'),
    (127, 'DMS_SEATBELT',       'Seatbelt not fastened (DMS)',          'important'),
    (128, 'DMS_EYE_CLOSED',     'Eyes closed (DMS)',                    'urgent'),
    (129, 'DMS_EVENT',          'Driver-monitoring event (DMS)',        'important'),
    -- Legacy first-level codes
    (100, 'DMS_FATIGUE',        'Driver fatigue (DMS)',                 'urgent'),
    (101, 'DMS_DISTRACTION',    'Driver distraction (DMS)',             'urgent'),
    (102, 'DMS_PHONE',          'Phone use while driving (DMS)',        'urgent'),
    (103, 'DMS_SMOKING',        'Smoking while driving (DMS)',          'important'),
    (104, 'DMS_NO_DRIVER',      'Driver absent (DMS)',                  'urgent'),
    (105, 'DMS_CAMERA_BLOCKED', 'Driver camera obstructed (DMS)',       'important'),
    (200, 'ADAS_FCW',           'Forward collision warning (ADAS)',     'urgent'),
    (201, 'ADAS_LDW',           'Lane departure (ADAS)',                'important'),
    (202, 'ADAS_HMW',           'Following too close (ADAS)',           'important'),
    (203, 'ADAS_PCW',           'Pedestrian collision warning (ADAS)',  'urgent')
),
parsed AS (
  SELECT a.id, (substring(a.alert_name FROM 'code ([0-9]+)'))::int AS code
  FROM alerts a
  WHERE a.alert_code = 'DASHCAM_EVENT'
    AND a.alert_name ~ 'code [0-9]+'
)
UPDATE alerts a
SET alert_code = m.alert_code,
    alert_name = m.name || ' · ' || p.code,
    severity   = m.severity
FROM parsed p
JOIN codemap m ON m.code = p.code
WHERE a.id = p.id;
