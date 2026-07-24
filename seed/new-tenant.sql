-- new-tenant.sql — provision a new customer tenant + its admin login.
--
-- Follows the pattern used for MAST: one tenant, one admin user, one default
-- fleet group. Tenancy is Postgres RLS, so a new tenant is what isolates a
-- customer's vehicles from every other customer's — a new *user* inside an
-- existing tenant would see that tenant's whole fleet.
--
-- Run as the DB owner (bypasses RLS). Idempotent: re-running is a no-op.
--
--   psql "$DSN" -v slug=brt -v name=BRT -v email=brt@karlo.id \
--               -v full_name='BRT Admin' -v password='<password>' \
--               -f seed/new-tenant.sql
--
-- Then import that tenant's trucks with the id this prints:
--   go run ./cmd/import -csv ../truck_inject_example.csv -tenant <id> -dsn "$DSN"

INSERT INTO tenants (name, slug)
VALUES (:'name', :'slug')
ON CONFLICT (slug) DO NOTHING;

-- bcrypt cost 12, matching internal/auth/service.go so the app can verify it.
INSERT INTO users (tenant_id, email, password_hash, full_name, role, status)
SELECT t.id, :'email', crypt(:'password', gen_salt('bf', 12)), :'full_name', 'admin', 'active'
FROM tenants t
WHERE t.slug = :'slug'
ON CONFLICT (email) DO NOTHING;

INSERT INTO fleet_groups (tenant_id, name, description)
SELECT t.id, 'Default Fleet', 'Default group'
FROM tenants t WHERE t.slug = :'slug'
ON CONFLICT (tenant_id, name) DO NOTHING;

\echo '--- provisioned ---'
SELECT t.id AS tenant_id, t.slug, u.email, u.full_name, u.role, u.status
FROM tenants t JOIN users u ON u.tenant_id = t.id
WHERE t.slug = :'slug';
