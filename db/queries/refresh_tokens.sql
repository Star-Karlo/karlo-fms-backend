-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (tenant_id, user_id, token_hash, expires_at, user_agent)
VALUES ($1, $2, $3, $4, sqlc.narg('user_agent'))
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = $1;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens
SET revoked_at = now(), replaced_by = sqlc.narg('replaced_by')
WHERE id = $1;

-- name: RevokeAllUserTokens :exec
UPDATE refresh_tokens
SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;
