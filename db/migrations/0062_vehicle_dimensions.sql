-- Lorry dimensions, so Valhalla can apply the restrictions it already knows.
--
-- OpenStreetMap carries height, width and weight limits on the roads; Valhalla
-- reads them; and FMS has never had the vehicle's own numbers to compare them
-- against, so every route is computed with fleet-wide defaults. A 40ft trailer
-- is routed like a van.
--
-- Added alongside 0057's axles because they are entered on the same screen and
-- come from the same place: master data, with vehiclesync projecting them onto
-- these columns. Nil upstream keeps whatever FMS holds, so an operator can fill
-- them in today without being overwritten on the next pass.
--
-- CONSTRAINED, BECAUSE THE LEGACY DATA IS NOT TRUSTWORTHY. The Mongo truck
-- master has dimensions on 96% of trucks at values including 23 m wide, 32 m
-- tall and 1.232 kg — form-filler noise that passes any "greater than zero"
-- check. Bounds here are generous enough for an Indonesian road train and
-- tight enough to reject a typo.
ALTER TABLE vehicles
  ADD COLUMN IF NOT EXISTS length_m      double precision,
  ADD COLUMN IF NOT EXISTS width_m       double precision,
  ADD COLUMN IF NOT EXISTS height_m      double precision,
  ADD COLUMN IF NOT EXISTS max_weight_kg double precision;

ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_dimensions_sane;
ALTER TABLE vehicles ADD CONSTRAINT vehicles_dimensions_sane CHECK (
      (length_m      IS NULL OR length_m      BETWEEN 2   AND 30)
  AND (width_m       IS NULL OR width_m       BETWEEN 1.2 AND 3.5)
  AND (height_m      IS NULL OR height_m      BETWEEN 1.2 AND 5.0)
  AND (max_weight_kg IS NULL OR max_weight_kg BETWEEN 500 AND 80000)
);

COMMENT ON COLUMN vehicles.length_m IS 'Overall length, metres. NULL = unknown; routing then uses fleet defaults.';
COMMENT ON COLUMN vehicles.width_m IS 'Overall width, metres. Compared against OSM maxwidth.';
COMMENT ON COLUMN vehicles.height_m IS 'Overall height, metres. Compared against OSM maxheight.';
COMMENT ON COLUMN vehicles.max_weight_kg IS 'Laden weight limit, kg. Compared against OSM maxweight.';
