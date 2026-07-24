-- 0027_platform_admin.sql — promote the first platform administrator.
--
-- WHY A NEW ROLE. Provisioning a customer (a tenant plus its first login) was
-- only possible by connecting to the database as the owner and running
-- seed/new-tenant.sql by hand. That needs the RDS master password and an open
-- admin CIDR, so in practice it could not be done at all from outside the VPC.
-- POST /v1/tenants replaces it — but tenant creation must NOT be reachable by
-- every customer's own administrator.
--
-- Every customer admin holds role 'admin'. Gating tenant creation on 'admin'
-- would let one customer provision tenants and users across the whole platform,
-- defeating the RLS isolation boundary. So 'platform_admin' sits one rank above
-- (see internal/auth/model.go) and is the only role that may create tenants.
--
-- users.role carries a CHECK constraint from 0002_auth.sql listing the three
-- original roles, so it has to be widened before the new value can be stored.
--
-- Idempotent: re-running promotes nobody new and changes nothing.

ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check
  CHECK (role IN ('operator', 'manager', 'admin', 'platform_admin'));

-- The platform operator is Karlo, who runs the service. Promote its seeded
-- administrator; every other admin (MAST, and any tenant created from here on)
-- deliberately stays at 'admin' and cannot reach the tenants endpoints.
UPDATE users u
SET role = 'platform_admin', updated_at = now()
FROM tenants t
WHERE u.tenant_id = t.id
  AND t.slug = 'karlo'
  AND u.email = 'admin@karlo.id'
  AND u.role <> 'platform_admin';

DO $$
DECLARE n int;
BEGIN
  SELECT count(*) INTO n FROM users WHERE role = 'platform_admin';
  IF n = 0 THEN
    -- Not fatal: a deployment whose seed admin was renamed still migrates, it
    -- just has nobody who can create tenants until one is promoted by hand.
    RAISE WARNING 'no platform_admin exists; promote one with: UPDATE users SET role=''platform_admin'' WHERE email=''<you>'';';
  ELSE
    RAISE NOTICE 'platform_admin count: %', n;
  END IF;
END $$;
