-- name: GetUserByEmail :one
-- Login bootstrap: runs unscoped (SystemTx); email is globally unique.
SELECT * FROM users WHERE email = $1;

-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1;

-- name: CreateUser :one
INSERT INTO users (tenant_id, email, password_hash, full_name, role, status)
VALUES ($1, $2, $3, $4, $5, COALESCE(sqlc.narg('status'), 'active'))
RETURNING *;

-- name: UpdateUser :one
UPDATE users
SET full_name = COALESCE(sqlc.narg('full_name'), full_name),
    role      = COALESCE(sqlc.narg('role'), role),
    status    = COALESCE(sqlc.narg('status'), status)
WHERE id = $1
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2 WHERE id = $1;

-- name: TouchUserLogin :exec
UPDATE users SET last_login_at = now() WHERE id = $1;

-- name: DeleteUser :exec
DELETE FROM users WHERE id = $1;

-- name: ListUsers :many
SELECT * FROM users
WHERE (sqlc.narg('q')::text IS NULL
       OR email ILIKE '%' || sqlc.narg('q') || '%'
       OR full_name ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CountUsers :one
SELECT count(*) FROM users
WHERE (sqlc.narg('q')::text IS NULL
       OR email ILIKE '%' || sqlc.narg('q') || '%'
       OR full_name ILIKE '%' || sqlc.narg('q') || '%')
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'));
