-- Operating-procedure parameters, per company (feedback epic 1.1).
--
-- "Kendaraan bergerak > X km/h selama lebih dari 30 menit", "berhenti > 5 jam",
-- "offline > 30 menit", "sensor tidak terbaca" — the thresholds are the
-- CUSTOMER's procedure, not Karlo's. One row per tenant, created on first
-- read with the defaults below; the notification engine's SOP pass reads it
-- every tick. fuel_price / idle_lph also live here so the fuel & cost figures
-- use the company's numbers instead of the engine defaults.
CREATE TABLE IF NOT EXISTS company_sop_settings (
  tenant_id            bigint PRIMARY KEY REFERENCES tenants(id),
  sop_enabled          boolean NOT NULL DEFAULT false, -- evaluate the SOP checks for every vehicle
  speed_limit_kmh      numeric NOT NULL DEFAULT 80,
  overspeed_min        int     NOT NULL DEFAULT 30,    -- sustained overspeed before it counts
  stop_max_min         int     NOT NULL DEFAULT 300,   -- 5 h stopped
  offline_min          int     NOT NULL DEFAULT 30,
  sensor_timeout_min   int     NOT NULL DEFAULT 60,    -- a sensor that stops reporting
  idle_min             int     NOT NULL DEFAULT 15,    -- idle counted from here
  fuel_price_per_litre numeric NOT NULL DEFAULT 6800,
  idle_lph             numeric NOT NULL DEFAULT 2.5,
  updated_by           bigint REFERENCES users(id),
  updated_at           timestamptz NOT NULL DEFAULT now()
);
