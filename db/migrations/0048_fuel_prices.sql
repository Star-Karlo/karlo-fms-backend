-- National fuel prices, platform-wide (one catalogue for every customer).
--
-- Subsidised Bio Solar is set by government decree and changes rarely
-- (Rp 6,800 since 3 Sep 2022); non-subsidised diesel moves monthly. There is
-- no official API, so Karlo staff record each change here once and every
-- company that follows the national price picks it up the same day. A
-- company can still fix its own price (company_sop_settings.fuel_price_per_litre
-- set) — a NULL there means "follow the national price".
CREATE TABLE IF NOT EXISTS fuel_prices (
  id             bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  fuel_type      text    NOT NULL,            -- biosolar | dexlite | pertamina_dex | solar_industri
  price          numeric NOT NULL,            -- Rp per litre
  effective_from date    NOT NULL,
  source         text,                        -- e.g. "Pertamina, Kepmen ESDM 245.K/2022"
  note           text,
  created_by     bigint REFERENCES users(id),
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (fuel_type, effective_from)
);
-- Only the price we can vouch for is seeded; non-subsidised types are added
-- by platform staff from Pertamina's monthly announcement.
INSERT INTO fuel_prices (fuel_type, price, effective_from, source, note) VALUES
  ('biosolar', 6800, '2022-09-03', 'Pertamina / Kepmen ESDM 2022', 'Subsidised Bio Solar, national price')
ON CONFLICT DO NOTHING;

ALTER TABLE company_sop_settings ALTER COLUMN fuel_price_per_litre DROP NOT NULL;
ALTER TABLE company_sop_settings ALTER COLUMN fuel_price_per_litre DROP DEFAULT;
ALTER TABLE company_sop_settings ADD COLUMN IF NOT EXISTS fuel_type text NOT NULL DEFAULT 'biosolar';
-- Rows created before this migration carried the old default (6800): that
-- was "the national price", not a choice — make them follow it.
UPDATE company_sop_settings SET fuel_price_per_litre = NULL WHERE fuel_price_per_litre = 6800;
