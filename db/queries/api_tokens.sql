-- name: ListApiTokens :many
SELECT * FROM api_tokens ORDER BY created_at DESC LIMIT sqlc.arg('lim') OFFSET sqlc.arg('off');

-- name: CreateApiToken :one
INSERT INTO api_tokens (tenant_id, name, token_prefix, token_hash, created_by)
VALUES ($1, $2, $3, $4, sqlc.narg('created_by'))
RETURNING *;

-- name: GetApiToken :one
SELECT * FROM api_tokens WHERE id = $1;

-- name: RevokeApiToken :exec
UPDATE api_tokens SET revoked_at = now() WHERE id = $1;

-- Deleting is the second step, for a key that is already revoked: the row goes
-- and the name with it. Revoking alone leaves a growing list of dead keys that
-- nobody dares tidy up, and "is this one still in use?" gets harder to answer
-- every month.
-- name: DeleteApiToken :execrows
DELETE FROM api_tokens WHERE id = $1;

-- The authenticator's own lookup, for keys FMS issued and therefore verifies.
-- An imported key (local_auth = false) is deliberately invisible here: the
-- shared auth service is authoritative for those, and matching a stale local
-- copy would let a key revoked upstream keep working.
-- name: FindLocalApiToken :one
SELECT id, tenant_id FROM api_tokens
WHERE token_hash = $1 AND local_auth AND revoked_at IS NULL;
