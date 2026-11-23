-- dashcam-autosave.sql — LOCAL ONLY, never for staging or production.
--
-- Gives the "karlo" tenant (seed.sql) two trucks carrying the fake camera
-- platform's cameras (karlo-fms-telemetry: make fake-vss), plus ~30 sample
-- automatic saves over the last 3 days, so Saved Evidence has every status to
-- show without telemetry or S3 running. Sample clips have no file in S3, so
-- View/Download won't play them. Run as the DB owner after seed.sql:
--
--   make seed-dashcam
--
-- Idempotent.

INSERT INTO vehicles (tenant_id, license_plate)
SELECT t.id, p.plate
FROM tenants t, (VALUES ('B 9101 CAM'), ('B 9102 CAM')) AS p(plate)
WHERE t.slug = 'karlo'
  AND NOT EXISTS (SELECT 1 FROM vehicles v WHERE v.tenant_id = t.id AND v.license_plate = p.plate);

INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, device_name, channels, channel_names, access_mode)
SELECT t.id, v.id, d.device, d.device, 2, 'Road;Driver', 5
FROM tenants t
JOIN (VALUES ('FAKE-CAM-1', 'B 9101 CAM'), ('FAKE-CAM-2', 'B 9102 CAM')) AS d(device, plate) ON true
JOIN vehicles v ON v.tenant_id = t.id AND v.license_plate = d.plate
WHERE t.slug = 'karlo'
ON CONFLICT DO NOTHING;

-- Sample saves on FAKE-CAM-2. clip_start is WIB clock time labelled UTC, as
-- every clip is stored. Alarm ids start with SEED- so they never meet the fake
-- platform's own events.
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start, clip_stop,
                         status, error, s3_bucket, s3_key, size_bytes, lat, lon, reason, alarm_id)
SELECT d.tenant_id, d.vehicle_id, d.vss_device_id, s.channel, s.start, s.start + interval '10 seconds',
       s.status,
       CASE WHEN s.status = 'failed' THEN 'no clip on server' END,
       CASE WHEN s.status = 'ready' THEN 'karlo-fms-local-video' END,
       CASE WHEN s.status = 'ready' THEN 'video/FAKE-CAM-2/seed/' || s.i || '.mp4' END,
       CASE WHEN s.status = 'ready' THEN CASE WHEN s.channel = 2 THEN 4960000 ELSE 740000 + s.i * 1300 END END,
       -6.2088 + s.i * 0.001, 106.8227 + s.i * 0.001,
       'auto:' || (ARRAY[124, 121, 122, 110, 116, 111, 117, 147, 133])[1 + s.i % 9],
       'SEED-' || s.i
FROM dashcams d
CROSS JOIN LATERAL (
  SELECT i,
         CASE i % 10 WHEN 0 THEN 'failed' WHEN 1 THEN 'no_clip' WHEN 2 THEN 'deleted' ELSE 'ready' END AS status,
         CASE WHEN i % 10 = 1 THEN 0 ELSE 1 + i % 2 END AS channel,
         ((now() AT TIME ZONE 'Asia/Jakarta')::date - (i % 3) + make_interval(hours => 6 + i % 12, mins => i))::timestamp
           AT TIME ZONE 'UTC' AS start
  FROM generate_series(1, 30) AS i
) s
WHERE d.vss_device_id = 'FAKE-CAM-2'
  AND d.tenant_id = (SELECT id FROM tenants WHERE slug = 'karlo')
  -- failed rows are outside the unique index, so ON CONFLICT alone would repeat them
  AND NOT EXISTS (SELECT 1 FROM video_clips c WHERE c.vss_device_id = d.vss_device_id AND c.alarm_id = 'SEED-' || s.i);

-- Edge cases on FAKE-CAM-1, today: one failed save per error the Saved
-- Evidence cards explain (the exact texts telemetry and the backend store),
-- an event with two cameras where only one saved, and an event the camera sent
-- nothing for that an SD-card pull (manual, linked by alarm_id) then saved.
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start, clip_stop,
                         status, error, s3_bucket, s3_key, size_bytes, reason, alarm_id, note)
SELECT d.tenant_id, d.vehicle_id, d.vss_device_id, e.channel, e.start, e.start + interval '10 seconds',
       e.status, e.error,
       CASE WHEN e.status = 'ready' THEN 'karlo-fms-local-video' END,
       CASE WHEN e.status = 'ready' THEN 'video/FAKE-CAM-1/seed/' || e.alarm_id || '-' || e.channel || '.mp4' END,
       CASE WHEN e.status = 'ready' THEN 2100000 END,
       e.reason, e.alarm_id,
       CASE WHEN e.reason = 'manual' THEN 'Pulled from SD card' END
FROM dashcams d
CROSS JOIN LATERAL (
  SELECT v.*, ((now() AT TIME ZONE 'Asia/Jakarta')::date + make_interval(hours => 7, mins => v.m))::timestamp
           AT TIME ZONE 'UTC' AS start
  FROM (VALUES
    ('SEED-ERR-GONE',    1, 'failed',  'no clip on server',                      'auto:121',  0),
    ('SEED-ERR-404',     1, 'failed',  'fetch clip: HTTP 404',                   'auto:124',  5),
    ('SEED-ERR-503',     1, 'failed',  'fetch clip: HTTP 503',                   'auto:116', 10),
    ('SEED-ERR-NET',     1, 'failed',  'fetch clip: Get "https://vss.example/clip.mp4": dial tcp: i/o timeout', 'auto:117', 15),
    ('SEED-ERR-EOF',     1, 'failed',  'buffer clip: unexpected EOF',            'auto:122', 20),
    ('SEED-ERR-SIGNAL',  1, 'failed',  'timed out preparing the clip — retry when the dashcam has stronger signal', 'auto:147', 25),
    ('SEED-ERR-TIMEOUT', 1, 'failed',  'the download timed out',                 'auto:133', 30),
    ('SEED-ERR-LOST',    1, 'failed',  'the download did not complete — please try again', 'auto:110', 35),
    ('SEED-ERR-403',     1, 'failed',  'fetch clip: HTTP 403',                   'auto:111', 40),
    ('SEED-ERR-CAMERA',  1, 'failed',  'the event belongs to another camera',    'auto:124', 45),
    ('SEED-ERR-OTHER',   1, 'failed',  'upload: AccessDenied: bucket policy',    'auto:121', 50),
    ('SEED-TWO-CAMS',    1, 'ready',   NULL,                                     'auto:116', 55),
    ('SEED-TWO-CAMS',    2, 'failed',  'fetch clip: HTTP 503',                   'auto:116', 55),
    ('SEED-PULLED',      0, 'no_clip', NULL,                                     'auto:121', 60),
    ('SEED-PULLED',      1, 'ready',   NULL,                                     'manual',   60)
  ) AS v(alarm_id, channel, status, error, reason, m)
) e
WHERE d.vss_device_id = 'FAKE-CAM-1'
  AND d.tenant_id = (SELECT id FROM tenants WHERE slug = 'karlo')
  AND NOT EXISTS (SELECT 1 FROM video_clips c
                  WHERE c.vss_device_id = d.vss_device_id AND c.alarm_id = e.alarm_id AND c.channel = e.channel);
