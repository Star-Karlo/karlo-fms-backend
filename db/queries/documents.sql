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
