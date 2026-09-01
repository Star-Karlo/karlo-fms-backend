-- 0041_platform_admin_root.sql — two-tier platform admin.
--
-- Until now every platform_admin was equal and all-powerful. This introduces a
-- ROOT tier ("the platform admin admin"): only a root may create, promote, edit
-- or delete other platform_admin accounts. A non-root ("child") platform_admin
-- still sees across all vendor accounts, but its actions are governed by its
-- assigned custom role (users.role_id → roles.permissions), and it cannot manage
-- other admins.
--
-- Non-breaking: every EXISTING platform_admin becomes root, so current behaviour
-- is unchanged. New child platform_admins (is_root = false) are the new tier.

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_root boolean NOT NULL DEFAULT false;

-- Existing platform admins keep full power (root). Anyone created from here on is
-- a child by default.
UPDATE users SET is_root = true WHERE role = 'platform_admin';
