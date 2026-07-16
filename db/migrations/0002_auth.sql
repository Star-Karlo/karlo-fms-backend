-- 0002_auth.sql — system users (login accounts) and refresh tokens.
--
-- `users` are system/login accounts (RBAC), distinct from `employees` (the
-- driver/staff registry in 0003). email is GLOBALLY unique so the login
-- endpoint can resolve a user (and thus their tenant) before any tenant scope
-- is set — see the login-bootstrap RLS policy in 0005_rls.sql.

CREATE TABLE users (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  email         text   NOT NULL UNIQUE,
  password_hash text   NOT NULL,                       -- bcrypt
  full_name     text,
  role          text   NOT NULL DEFAULT 'operator'
                CHECK (role IN ('operator','manager','admin')),
  status        text   NOT NULL DEFAULT 'active'
                CHECK (status IN ('active','disabled')),
  last_login_at timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_users_tenant ON users (tenant_id);
CREATE TRIGGER trg_users_touch BEFORE UPDATE ON users
  FOR EACH ROW EXECUTE FUNCTION touch_updated_at();

-- Refresh tokens are stored HASHED (sha-256), never in plaintext. Rotation:
-- refreshing revokes the presented token and links replaced_by to the new one,
-- so a replayed (already-revoked) token can be detected and the chain revoked.
CREATE TABLE refresh_tokens (
  id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id   bigint NOT NULL REFERENCES tenants(id),
  user_id     bigint NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash  bytea  NOT NULL UNIQUE,                  -- sha256(refresh token)
  expires_at  timestamptz NOT NULL,
  revoked_at  timestamptz,
  replaced_by bigint REFERENCES refresh_tokens(id),
  user_agent  text,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_refresh_user ON refresh_tokens (user_id);
