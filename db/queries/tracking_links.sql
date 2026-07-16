-- name: ListTrackingLinks :many
SELECT * FROM tracking_links
WHERE (sqlc.narg('q')::text IS NULL OR name ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountTrackingLinks :one
SELECT count(*) FROM tracking_links
WHERE (sqlc.narg('q')::text IS NULL OR name ILIKE '%' || sqlc.narg('q') || '%');

-- name: GetTrackingLink :one
SELECT * FROM tracking_links WHERE id = $1;

-- name: CreateTrackingLink :one
INSERT INTO tracking_links (tenant_id, token, name, note, status, expires_at, show_fields, vehicle_ids)
VALUES ($1, $2, $3, sqlc.narg('note'), COALESCE(sqlc.narg('status'),'active'), $4,
        COALESCE(sqlc.narg('show_fields')::jsonb, '{}'::jsonb), COALESCE(sqlc.narg('vehicle_ids')::jsonb, '[]'::jsonb))
RETURNING *;

-- name: UpdateTrackingLink :one
UPDATE tracking_links
SET name        = COALESCE(sqlc.narg('name'), name),
    note        = COALESCE(sqlc.narg('note'), note),
    status      = COALESCE(sqlc.narg('status'), status),
    expires_at  = COALESCE(sqlc.narg('expires_at'), expires_at),
    show_fields = COALESCE(sqlc.narg('show_fields')::jsonb, show_fields),
    vehicle_ids = COALESCE(sqlc.narg('vehicle_ids')::jsonb, vehicle_ids)
WHERE id = $1
RETURNING *;

-- name: DeleteTrackingLink :exec
DELETE FROM tracking_links WHERE id = $1;

-- name: GetTrackingLinkByToken :one
-- Public lookup (no tenant scope) — used by the share endpoint.
SELECT * FROM tracking_links WHERE token = $1;
