-- Alert lifecycle: Open -> Followed up -> Resolved / False alert, with WHO.
--
-- check_status gains 'followed_up' (someone has picked it up: called the
-- driver, dispatched help) between 'unchecked' (Open) and 'checked'
-- (Resolved). checked_by records the user who last changed it, so a fleet
-- manager sees not just that an incident was handled but by whom.
ALTER TABLE alerts ADD COLUMN IF NOT EXISTS checked_by bigint REFERENCES users(id);
CREATE INDEX IF NOT EXISTS alerts_open_idx ON alerts (tenant_id, occurred_at DESC) WHERE check_status IN ('unchecked', 'followed_up');
