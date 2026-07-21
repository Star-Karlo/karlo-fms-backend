-- 0023_report_schedules.sql — email a saved report on a cadence.
--
-- THE PERIOD LIVES HERE, NOT ON THE REPORT. A saved report is a template whose
-- window is chosen per run; a schedule chooses it from its own cadence — daily
-- means yesterday, weekly means the last seven days, monthly means last month.
-- That is what lets ONE saved report go weekly to operations and monthly to
-- management without being duplicated, and it is why a schedule cannot simply
-- reuse whatever dates the report was saved with: it would email the same
-- frozen week forever, and look entirely normal doing it.
--
-- Idempotent: scripts/migrate.sh re-applies every file on every deploy.

CREATE TABLE IF NOT EXISTS report_schedules (
  id            bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  tenant_id     bigint      NOT NULL REFERENCES tenants(id),
  user_id       bigint      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  report_id     bigint      NOT NULL REFERENCES report_definitions(id) ON DELETE CASCADE,

  cadence       text        NOT NULL,              -- daily | weekly | monthly
  day_of_week   int,                               -- 0=Sunday, weekly only
  day_of_month  int,                               -- 1-28, monthly only
  hour          int         NOT NULL DEFAULT 7,    -- local hour, 0-23
  timezone      text        NOT NULL DEFAULT 'Asia/Jakarta',

  format        text        NOT NULL DEFAULT 'pdf',
  recipients    text[]      NOT NULL,
  subject       text        NOT NULL DEFAULT '',

  enabled       boolean     NOT NULL DEFAULT true,

  -- next_run_at is the claim token. The runner updates it inside the same
  -- transaction that selects the row, so two API instances cannot both send
  -- the same report — the second finds nothing due.
  next_run_at   timestamptz NOT NULL,
  last_run_at   timestamptz,
  -- Failure has to be VISIBLE. A schedule that silently stops sending is worse
  -- than one that was never created, because nobody goes looking for an email
  -- that simply never arrives.
  last_status   text        NOT NULL DEFAULT '',   -- ok | error
  last_error    text        NOT NULL DEFAULT '',

  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT report_schedules_cadence CHECK (cadence IN ('daily','weekly','monthly')),
  CONSTRAINT report_schedules_hour CHECK (hour BETWEEN 0 AND 23),
  CONSTRAINT report_schedules_dow CHECK (day_of_week IS NULL OR day_of_week BETWEEN 0 AND 6),
  -- 28, not 31: a schedule for the 30th would skip February entirely, which is
  -- the kind of gap nobody notices until a month-end report is missing.
  CONSTRAINT report_schedules_dom CHECK (day_of_month IS NULL OR day_of_month BETWEEN 1 AND 28),
  CONSTRAINT report_schedules_format CHECK (format IN ('xlsx','pdf','docx','csv')),
  CONSTRAINT report_schedules_recipients CHECK (cardinality(recipients) BETWEEN 1 AND 20)
);

-- The runner's only query: what is due. Partial, because disabled schedules are
-- never due and there is no reason to walk them.
CREATE INDEX IF NOT EXISTS idx_report_schedules_due
  ON report_schedules (next_run_at) WHERE enabled;

CREATE INDEX IF NOT EXISTS idx_report_schedules_owner
  ON report_schedules (tenant_id, user_id, updated_at DESC);

ALTER TABLE report_schedules ENABLE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON report_schedules;
CREATE POLICY tenant_isolation ON report_schedules
  USING (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.current_tenant', true), '')::bigint);

GRANT SELECT, INSERT, UPDATE, DELETE ON report_schedules TO fms_app;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO fms_app;
