-- 0025_company_profile.sql — company details on the tenant record.
--
-- The tenants table has held only (name, slug) since 0001. These columns are
-- what a fleet operator needs on a printed report or a delivery note: who the
-- company legally is, where it is, how to reach it, and its NPWP.
--
-- SECURITY NOTE, because this table is the exception to the house rule.
--
-- `tenants` has NO row-level security — deliberately, since login has to
-- resolve a tenant before any tenant scope exists, and it is the one table
-- whose rows are not tenant-owned data but the tenants themselves. Every other
-- table in this schema is protected by RLS, so a missing WHERE clause is
-- caught by the database. Here it is not: an UPDATE without an explicit
-- `WHERE id = <caller's tenant>` would happily rewrite every customer's
-- company profile.
--
-- internal/company therefore scopes every statement on auth.TenantID(ctx) by
-- hand and never accepts a tenant id from the request body.

ALTER TABLE tenants
  ADD COLUMN IF NOT EXISTS legal_name  text,   -- "PT Karlo Logistik Indonesia"
  ADD COLUMN IF NOT EXISTS tax_id      text,   -- NPWP
  ADD COLUMN IF NOT EXISTS address     text,
  ADD COLUMN IF NOT EXISTS city        text,
  ADD COLUMN IF NOT EXISTS province    text,
  ADD COLUMN IF NOT EXISTS postal_code text,
  ADD COLUMN IF NOT EXISTS country     text,
  ADD COLUMN IF NOT EXISTS phone       text,
  ADD COLUMN IF NOT EXISTS email       text,
  ADD COLUMN IF NOT EXISTS website     text,
  -- Key into the documents bucket, not a URL: the bucket is private and served
  -- only through presigned links, so storing a URL would bake in one that
  -- expires within minutes.
  ADD COLUMN IF NOT EXISTS logo_key    text,
  ADD COLUMN IF NOT EXISTS updated_at  timestamptz NOT NULL DEFAULT now();

-- fms_app already holds SELECT/UPDATE on tenants from the 0005 grants; no
-- change needed here. Stated explicitly so the absence is not read as an
-- oversight.
