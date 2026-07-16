-- seed.sql — default tenant + admin login so a dev can authenticate immediately.
-- Password: admin123 (bcrypt via pgcrypto). Change in any real environment.
-- Run as the DB owner (bypasses RLS).

INSERT INTO tenants (name, slug)
VALUES ('Karlo', 'karlo')
ON CONFLICT (slug) DO NOTHING;

INSERT INTO users (tenant_id, email, password_hash, full_name, role)
SELECT t.id, 'admin@karlo.id', crypt('admin123', gen_salt('bf', 12)), 'Fleet Admin', 'admin'
FROM tenants t
WHERE t.slug = 'karlo'
ON CONFLICT (email) DO NOTHING;

-- A demo fleet group + operator for convenience.
INSERT INTO fleet_groups (tenant_id, name, description)
SELECT t.id, 'Default Fleet', 'Seeded default group'
FROM tenants t WHERE t.slug = 'karlo'
ON CONFLICT (tenant_id, name) DO NOTHING;

INSERT INTO users (tenant_id, email, password_hash, full_name, role)
SELECT t.id, 'operator@karlo.id', crypt('operator123', gen_salt('bf', 12)), 'Dispatch Operator', 'operator'
FROM tenants t WHERE t.slug = 'karlo'
ON CONFLICT (email) DO NOTHING;
