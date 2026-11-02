-- 0064_tracker_owner.sql — record who OWNS each GPS tracker.
--
-- Master data has carried this since the device cutover: trackers.owner is
-- karlo | customer | vendor, with ownerName for the third party, and its own
-- comment gives the reason — "without the distinction nobody can answer
-- 'which of our units are out with customers'". The gRPC contract already
-- ships both fields. Only the projection dropped them, so FMS, and every
-- analysis keyed on FMS, could not tell our own hardware from a vendor's
-- feed. That split is the first thing fleet-quality analysis needs: a
-- vendor's gaps are not ours to fix, and averaging the two hides both.
--
-- Nullable with no default on purpose. A device whose owner master data does
-- not state stays unknown; defaulting to 'karlo' would silently claim every
-- unlabelled unit as ours, which is the error this column exists to prevent.
--
-- Master data is the only writer (there is no FMS UI for ownership), so
-- trackersync projects these authoritatively rather than merging.
--
-- Reversal: both columns are additive and nothing else reads them yet, so
-- dropping them loses only the projection, which the next sync pass rebuilds.
ALTER TABLE trackers ADD COLUMN IF NOT EXISTS owner      text;
ALTER TABLE trackers ADD COLUMN IF NOT EXISTS owner_name text;

-- Re-added rather than guarded, so re-running the file is safe.
ALTER TABLE trackers DROP CONSTRAINT IF EXISTS trackers_owner_check;
ALTER TABLE trackers
  ADD CONSTRAINT trackers_owner_check
  CHECK (owner IS NULL OR owner IN ('karlo', 'customer', 'vendor'));

-- "Which of this account's devices are ours" is the question asked of the
-- column, so the index leads with the tenant.
CREATE INDEX IF NOT EXISTS idx_trackers_owner ON trackers (tenant_id, owner);
