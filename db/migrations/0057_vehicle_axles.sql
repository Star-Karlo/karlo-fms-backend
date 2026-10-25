-- Axle count on a vehicle, which is what toll is priced on.
--
-- Indonesian toll tariffs are keyed on (entry gate, exit gate, GOLONGAN), and
-- golongan for a goods vehicle is its axle count: 2 -> II, 3 -> III, 4 -> IV,
-- 5 or more -> V. Without this column a route's toll can be computed but not
-- attributed to a class, so the answer is five numbers rather than one.
--
-- It is also what the existing paid integration already uses: TMS's TollGuru
-- client sends `axles` directly, so this is not a new concept being invented
-- here, it is a field FMS was missing.
--
-- NULLABLE, AND NO DEFAULT, ON PURPOSE. The legacy truck master in Mongo has
-- an axle on 53% of live rows (710 of 1.340) and some of those contradict the
-- vehicle: one record carries axle 3 beside a `plannerIcon` of
-- "Pickup_2_other", and a pickup is Golongan I with two axles. Defaulting the
-- other 47% to any value would misprice every truck that has one, silently.
-- NULL means unknown and callers must say so.
--
-- truckType CANNOT fill the gap: 17 of the 24 types in that data span several
-- axle counts, so there is no type -> axle rule to infer from.
ALTER TABLE vehicles
  ADD COLUMN IF NOT EXISTS axles smallint,
  -- Overrides the axle-derived class where someone who has seen the truck
  -- knows better. Two axles is the case that needs it: BPJT splits those
  -- between Golongan I ("pick up / truk kecil") and Golongan II (a 2-axle
  -- lorry) on size, which an axle count alone cannot distinguish.
  ADD COLUMN IF NOT EXISTS golongan_override smallint;

-- Physically meaningless values are rejected rather than stored and worked
-- around later. The legacy data also holds dimensions like 23 m wide and 32 m
-- tall — form-filler noise that passes a "greater than zero" check — which is
-- the argument for constraining at the column.
ALTER TABLE vehicles
  DROP CONSTRAINT IF EXISTS vehicles_axles_sane;
ALTER TABLE vehicles
  ADD CONSTRAINT vehicles_axles_sane
  CHECK (axles IS NULL OR (axles BETWEEN 2 AND 12));

ALTER TABLE vehicles
  DROP CONSTRAINT IF EXISTS vehicles_golongan_override_sane;
ALTER TABLE vehicles
  ADD CONSTRAINT vehicles_golongan_override_sane
  CHECK (golongan_override IS NULL OR (golongan_override BETWEEN 1 AND 5));

COMMENT ON COLUMN vehicles.axles IS
  'Axle count. Toll golongan derives from it: 2=II, 3=III, 4=IV, 5+=V. NULL means unknown — never default it.';
COMMENT ON COLUMN vehicles.golongan_override IS
  'Manual toll class, where axles cannot decide it. Mainly 2-axle vehicles, which split between Golongan I and II on size.';
