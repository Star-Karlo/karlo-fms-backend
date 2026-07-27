-- 0032_webhooks.sql — outbound alarm webhooks: push every alert to a
-- tenant-configured URL, HMAC-signed, with retry.
--
-- The alerts table is the single chokepoint every alarm flows through — camera
-- DMS/ADAS/G-sensor events (via the dashcam alarm poller) and GPS rule events
-- (via the notification engine) both land there. So one dispatcher that watches
-- alerts delivers "everything" without hooking each producer.
--
-- Two tables:
--   webhook_endpoints  — the destination(s) a tenant configures in Settings,
--                        plus a per-endpoint cursor so a new endpoint does not
--                        replay old history.
--   webhook_deliveries — one row per (endpoint, alert): the delivery attempt log
--                        that drives retry/backoff and shows status in the UI.
--
-- Idempotent.

CREATE TABLE IF NOT EXISTS webhook_endpoints (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  url           text   NOT NULL,
  -- Shared secret for the HMAC-SHA256 signature sent in X-FMS-Signature, so the
  -- receiver can verify the payload is from us and untampered.
  secret        text   NOT NULL DEFAULT '',
  enabled       boolean NOT NULL DEFAULT true,
  -- Reserved for per-type filtering; 'all' delivers every alert.
  event_filter  text   NOT NULL DEFAULT 'all',
  -- Highest alert id already enqueued for this endpoint. New endpoints start at
  -- the current max so they do not flood with historical alerts.
  last_alert_id bigint NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint NOT NULL REFERENCES tenants(id),
  endpoint_id   bigint NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
  alert_id      bigint NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
  status        text   NOT NULL DEFAULT 'pending', -- pending | success | failed
  attempts      int    NOT NULL DEFAULT 0,
  last_status   int,                                -- last HTTP status code
  last_error    text,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);

-- One delivery per (endpoint, alert).
CREATE UNIQUE INDEX IF NOT EXISTS webhook_deliveries_uk
  ON webhook_deliveries (endpoint_id, alert_id);
-- The dispatcher scans "due, not yet succeeded" work.
CREATE INDEX IF NOT EXISTS webhook_deliveries_due_idx
  ON webhook_deliveries (status, next_attempt_at);

ALTER TABLE webhook_endpoints  ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON webhook_endpoints;
CREATE POLICY tenant_isolation ON webhook_endpoints
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);
DROP POLICY IF EXISTS tenant_isolation ON webhook_deliveries;
CREATE POLICY tenant_isolation ON webhook_deliveries
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_endpoints  TO fms_app;
GRANT SELECT, INSERT, UPDATE, DELETE ON webhook_deliveries TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;

DO $$ BEGIN RAISE NOTICE 'webhooks ready'; END $$;
