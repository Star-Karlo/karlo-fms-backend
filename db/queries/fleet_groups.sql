-- name: CreateFleetGroup :one
INSERT INTO fleet_groups (tenant_id, name, description)
VALUES ($1, $2, sqlc.narg('description'))
RETURNING *;

-- name: GetFleetGroup :one
SELECT * FROM fleet_groups WHERE id = $1;

-- name: UpdateFleetGroup :one
UPDATE fleet_groups
SET name        = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description)
WHERE id = $1
RETURNING *;

-- name: DeleteFleetGroup :exec
DELETE FROM fleet_groups WHERE id = $1;

-- name: ListFleetGroups :many
SELECT * FROM fleet_groups
WHERE (sqlc.narg('q')::text IS NULL OR name ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY name
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountFleetGroups :one
SELECT count(*) FROM fleet_groups
WHERE (sqlc.narg('q')::text IS NULL OR name ILIKE '%' || sqlc.narg('q') || '%');
