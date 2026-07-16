-- name: ListApiTokens :many
SELECT * FROM api_tokens ORDER BY created_at DESC LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CreateApiToken :one
INSERT INTO api_tokens (tenant_id, name, token_prefix, token_hash, created_by)
VALUES ($1, $2, $3, $4, sqlc.narg('created_by'))
RETURNING *;

-- name: RevokeApiToken :exec
UPDATE api_tokens SET revoked_at = now() WHERE id = $1;
