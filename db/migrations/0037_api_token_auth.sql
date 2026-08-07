-- 0037_api_token_auth.sql — let the API-token authenticator resolve a token to
-- its tenant BEFORE any tenant scope exists.
--
-- api_tokens shipped with the strict tenant_isolation policy, so a pre-tenant
-- (SystemTx) lookup by token_hash saw zero rows and no authenticator could be
-- wired. Switch it to the system-permitting variant already used elsewhere
-- (see 0005_rls.sql): an unset tenant (the auth bootstrap) may read; otherwise
-- rows are still confined to the caller's tenant. Tenant isolation for normal
-- (JWT) traffic is unchanged.
DROP POLICY IF EXISTS tenant_isolation ON api_tokens;
CREATE POLICY tenant_isolation ON api_tokens
  USING (NULLIF(current_setting('app.current_tenant', true), '') IS NULL
         OR tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (NULLIF(current_setting('app.current_tenant', true), '') IS NULL
         OR tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);
