-- name: ListDocuments :many
SELECT
  d.*,
  v.license_plate AS vehicle_plate,
  e.full_name     AS driver_name,
  (CASE
     WHEN d.expiry IS NULL THEN 'unidentified'
     WHEN d.expiry < current_date THEN 'expired'
     WHEN d.expiry < current_date + 30 THEN 'will_expire'
     ELSE 'valid'
   END) AS doc_status
FROM documents d
LEFT JOIN vehicles v  ON d.owner_type = 'vehicle' AND v.id = d.owner_id
LEFT JOIN employees e ON d.owner_type = 'driver'  AND e.id = d.owner_id
WHERE (sqlc.narg('owner_type')::text IS NULL OR d.owner_type = sqlc.narg('owner_type'))
  AND (sqlc.narg('q')::text IS NULL OR d.number ILIKE '%' || sqlc.narg('q') || '%'
       OR d.kind ILIKE '%' || sqlc.narg('q') || '%' OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR
       (CASE
          WHEN d.expiry IS NULL THEN 'unidentified'
          WHEN d.expiry < current_date THEN 'expired'
          WHEN d.expiry < current_date + 30 THEN 'will_expire'
          ELSE 'valid'
        END) = sqlc.narg('status'))
ORDER BY d.expiry ASC NULLS LAST
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountDocuments :one
SELECT count(*) FROM documents d
LEFT JOIN vehicles v ON d.owner_type = 'vehicle' AND v.id = d.owner_id
WHERE (sqlc.narg('owner_type')::text IS NULL OR d.owner_type = sqlc.narg('owner_type'))
  AND (sqlc.narg('q')::text IS NULL OR d.number ILIKE '%' || sqlc.narg('q') || '%'
       OR d.kind ILIKE '%' || sqlc.narg('q') || '%' OR v.license_plate ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR
       (CASE
          WHEN d.expiry IS NULL THEN 'unidentified'
          WHEN d.expiry < current_date THEN 'expired'
          WHEN d.expiry < current_date + 30 THEN 'will_expire'
          ELSE 'valid'
        END) = sqlc.narg('status'));

-- name: DocumentStatusCounts :one
SELECT
  count(*) FILTER (WHERE expiry < current_date) AS expired,
  count(*) FILTER (WHERE expiry >= current_date AND expiry < current_date + 30) AS will_expire,
  count(*) FILTER (WHERE expiry >= current_date + 30) AS valid,
  count(*) FILTER (WHERE expiry IS NULL) AS unidentified
FROM documents
WHERE (sqlc.narg('owner_type')::text IS NULL OR owner_type = sqlc.narg('owner_type'));

-- name: ListExpiringDocuments :many
-- Documents whose expiry falls within the next @within_days days (or has
-- already passed but not by more than 30 days — an expired STNK still needs
-- chasing), with the vehicle to file the alert against: the document's own
-- vehicle, or for a driver's document, the vehicle that driver currently
-- drives. Feeds the daily document-expiry pass of the notification engine.
SELECT d.id, d.owner_type, d.owner_id, d.kind, d.title, d.number, d.expiry,
       COALESCE(v.id, dv.id, 0)::bigint                     AS vehicle_id,     -- 0 = no vehicle to file against
       COALESCE(v.license_plate, dv.license_plate, '')::text AS license_plate,
       e.full_name                                 AS driver_name,
       (d.expiry - CURRENT_DATE)::int              AS days_left
FROM documents d
LEFT JOIN vehicles v   ON d.owner_type = 'vehicle' AND v.id = d.owner_id AND v.tenant_id = d.tenant_id
LEFT JOIN employees e  ON d.owner_type = 'driver'  AND e.id = d.owner_id AND e.tenant_id = d.tenant_id
LEFT JOIN vehicles dv  ON d.owner_type = 'driver'  AND dv.current_driver_id = e.id AND dv.tenant_id = d.tenant_id
WHERE d.tenant_id = @tenant_id
  AND d.expiry IS NOT NULL
  AND d.expiry <= CURRENT_DATE + @within_days::int
  AND d.expiry >= CURRENT_DATE - 30
ORDER BY d.expiry;

-- name: CountRecentDocumentAlert :one
-- Dedupe for document alerts, keyed on the document (in limit_value) rather
-- than the vehicle: two documents on one truck are two reminders.
SELECT count(*) FROM alerts
WHERE tenant_id = @tenant_id AND alert_code = @alert_code
  AND limit_value = @document_id::numeric
  AND occurred_at > now() - make_interval(days => @cooldown_days::int);
