-- 0030_vehicle_fuel.sql — per-vehicle fuel figures for consumption estimation.
--
-- No fuel SENSOR is involved. Given the tank size and the factory efficiency
-- (km per litre), fuel used over a distance is just distance / ratio — a
-- planning estimate, not a measurement. Both are optional: when a vehicle has
-- neither, nothing about fuel is shown for it, so the estimate never masquerades
-- as a reading the truck cannot actually produce.
--
-- Idempotent.

ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS fuel_tank_liters double precision;
ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS fuel_ratio_kmpl  double precision;

-- Guard against a zero ratio: it is a divide-by-zero waiting to happen in every
-- estimate, and a truck that does 0 km/L is a data-entry slip, not a fact.
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_fuel_ratio_positive;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_fuel_ratio_positive
  CHECK (fuel_ratio_kmpl IS NULL OR fuel_ratio_kmpl > 0);
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_fuel_tank_positive;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_fuel_tank_positive
  CHECK (fuel_tank_liters IS NULL OR fuel_tank_liters > 0);

DO $$ BEGIN RAISE NOTICE 'vehicle fuel columns ready'; END $$;
