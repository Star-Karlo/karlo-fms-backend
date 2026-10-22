-- 0054_api_token_local_auth.sql — which store owns each API key.
--
-- THE BUG. Since the SSO cutover, a key minted in FMS settings has never
-- worked. Creating one writes a row here; authenticating one asks the shared
-- auth service, which has never heard of it — so every key made since then was
-- dead the moment it was issued, and said so with "invalid or revoked API
-- token", which reads like the key was cancelled rather than never registered.
--
-- Checking both stores was deliberately avoided, for a good reason: the
-- importer copied the then-existing keys' hashes into karlo_auth.api_keys, and
-- for those the upstream row is authoritative. Accepting a local row as well
-- would let a key REVOKED upstream keep working here for as long as the stale
-- copy survived.
--
-- Both facts can hold at once, because the two sets of keys are disjoint and
-- knowable. Everything in this table TODAY is a key the importer copied
-- upstream; everything created from now on is FMS's own. One column records
-- which, so the authenticator can ask the right store for each key instead of
-- choosing one policy for both.

ALTER TABLE api_tokens
  ADD COLUMN IF NOT EXISTS local_auth boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN api_tokens.local_auth IS
  'true: FMS issued this key and verifies it here. false: the shared auth service owns it and is authoritative — a local row is only a copy for listing.';

-- Rows that exist now are the imported ones, and keep local_auth = false.
-- Everything inserted after this point is FMS''s own.
ALTER TABLE api_tokens ALTER COLUMN local_auth SET DEFAULT true;

-- The authenticator's lookup: by hash, only live keys it is allowed to verify.
CREATE INDEX IF NOT EXISTS api_tokens_local_live ON api_tokens (token_hash)
  WHERE local_auth AND revoked_at IS NULL;
