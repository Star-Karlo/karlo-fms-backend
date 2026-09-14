-- 0044_remap_camera_alert_codes.sql — relabel camera alerts already stored
-- under the wrong meaning.
--
-- The dashcam code table (internal/dashcam/codes.go) was a guessed "standard"
-- mapping, and it was wrong for nearly the whole DMS range. Howen's own VSS
-- API sheet says 123 is "Camera cover"; FMS had it as "Smoking". A driver
-- covered the lens and the feed said they lit a cigarette. 121 was labelled
-- fatigue and is eyes-closing, 122 phone and is yawning, 125 camera-blocked
-- and is seatbelt, and the 200-203 block filed as ADAS collision warnings is
-- the geofence enter/exit/overspeed range.
--
-- codes.go is corrected in the same deploy. This fixes what is ALREADY in the
-- table, which the code change cannot: alerts are stored with the resolved
-- alert_code and name, not the raw device code. Fortunately the raw code was
-- kept in the name ("Smoking while driving (DMS) · 123") precisely so the
-- mapping could be verified later — and that suffix is what this keys on.
--
-- Only codes the OLD table knew are touched. A code it did not know was
-- stored as the honest "Camera event (code N)", which the new table will
-- render properly from now on but which was never a lie.
--
-- Idempotent: keyed on the raw number, not the current label, so a second run
-- recomputes the same answer.

WITH remap(raw, code, name, severity) AS (VALUES
  -- old table had 1 as PANIC; the sheet says 1 is video lost, panic is 5
  (1,   'CAM_VIDEO_LOST',     'Video signal lost',                         'important'),
  (24,  'HARSH_ACCEL',        'Harsh acceleration',                        'important'),
  (25,  'HARSH_BRAKING',      'Harsh braking',                             'important'),
  (26,  'LOW_SPEED',          'Low speed warning',                         'important'),
  (27,  'OVERSPEED',          'High speed warning',                        'important'),
  -- "legacy 10x" block: actually ADAS
  (100, 'ADAS_HMW',           'Safe distance (ADAS)',                      'important'),
  (102, 'ADAS_LDW',           'Lane departure warning (ADAS)',             'important'),
  (103, 'ADAS_HMW',           'Front vehicle too close (ADAS)',            'important'),
  (104, 'ADAS_PCW',           'Pedestrian collision warning (ADAS)',       'urgent'),
  -- 11x
  (110, 'ADAS_FCW',           'Forward collision warning (ADAS)',          'urgent'),
  (111, 'ADAS_HMW',           'Front vehicle too close (ADAS)',            'important'),
  (115, 'DMS_FATIGUE',        'Fatigue driving (DMS)',                     'urgent'),
  (116, 'DMS_PHONE',          'Phone call while driving (DMS)',            'urgent'),
  (117, 'DMS_SMOKING',        'Smoking while driving (DMS)',               'important'),
  -- 12x — the block that produced the complaint
  (121, 'DMS_EYE_CLOSED',     'Eyes closing (DMS)',                        'urgent'),
  (122, 'DMS_YAWN',           'Yawning (DMS)',                             'important'),
  (123, 'DMS_CAMERA_BLOCKED', 'Camera cover (DMS)',                        'important'),
  (124, 'DMS_DISTRACTION',    'Distracted — glancing left/right (DMS)',    'urgent'),
  (125, 'DMS_SEATBELT',       'Seatbelt not worn (DMS)',                   'important'),
  (126, 'DMS_NO_DRIVER',      'Driver left duty (DMS)',                    'urgent'),
  (127, 'DMS_DRINKING',       'Drinking water (DMS)',                      'important'),
  -- "legacy 20x" block: actually geofence
  (200, 'GEOFENCE_ENTER',     'Geofence entered',                          'important'),
  (201, 'GEOFENCE_EXIT',      'Geofence exited',                           'important'),
  (202, 'OVERSPEED',          'Geofence over-speed alarm',                 'urgent'),
  (203, 'OVERSPEED',          'Geofence over-speed warning',               'important')
)
UPDATE alerts a
   SET alert_code = r.code,
       alert_name = r.name || ' · ' || r.raw,
       severity   = r.severity
  FROM remap r
 WHERE a.device_source = 'camera'
   AND a.alert_name ~ ' · [0-9]+$'
   AND substring(a.alert_name from ' · ([0-9]+)$')::int = r.raw
   AND (a.alert_code IS DISTINCT FROM r.code OR a.alert_name IS DISTINCT FROM r.name || ' · ' || r.raw);

-- Codes the old table mapped that the sheet does NOT list at all (101, 105,
-- 112, 113, 114, 128) cannot be given a meaning. They become the honest
-- generic rather than keep a label that was invented.
UPDATE alerts a
   SET alert_code = 'DASHCAM_EVENT',
       alert_name = 'Camera event (code ' || substring(a.alert_name from ' · ([0-9]+)$') || ')',
       severity   = 'important'
 WHERE a.device_source = 'camera'
   AND a.alert_name ~ ' · [0-9]+$'
   AND substring(a.alert_name from ' · ([0-9]+)$')::int IN (101, 105, 112, 113, 114, 128)
   AND a.alert_code <> 'DASHCAM_EVENT';

-- Housekeeping events that should never have been alerts (driver change 120,
-- driver returns 129) are removed. The poller now drops them on arrival.
DELETE FROM alerts
 WHERE device_source = 'camera'
   AND alert_name ~ ' · (120|129)$';
