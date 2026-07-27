-- Populate vehicle fuel tank capacity + fuel ratio from researched values.
--
-- Values are keyed by (manufacturer, class): class is the prime mover, derived
-- from vehicle_type — a "Trailer …" body means a tractor head, "Tronton …"
-- means a rigid truck. Tank capacities are published chassis specs; the km/L
-- ratio is an operational estimate for loaded Indonesian container haulage and
-- should be refined from real fuel logs.
--
-- COALESCE keeps any value already entered — this only fills blanks, so it is
-- safe to run more than once and never clobbers a manually corrected figure.
--
-- Sources (2026): FAW JH6 spec sheet · SHACMAN X3000 standard 400L config ·
-- Mercedes Axor 2528 R 6x4 official brochure (290L) · DAF XF FTT (configurable,
-- 600L standard) · Hino 500 FM 260 JD official ID (200L) · Hino 700 SS single
-- tank · Volvo FM 420 6x4 data sheet (405L).
--
-- RLS: run per tenant. Set the tenant first, e.g. from psql:
--   SET app.current_tenant = '1';  \i fill-vehicle-fuel.sql
-- or wrap the whole file per tenant in a loop (see the runbook the assistant
-- provided). Owner/superuser connections that bypass RLS can run it once.

--                                                    tank(L)  km/L
UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 400),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.3)
 WHERE manufacturer = 'FAW'           AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 400),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.3)
 WHERE manufacturer = 'SHACMAN'       AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 290),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.6)
 WHERE manufacturer = 'MERCEDES-BENZ' AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 600),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.7)
 WHERE manufacturer = 'DAF'           AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 400),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.5)
 WHERE manufacturer = 'Hino'          AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 405),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  2.7)
 WHERE manufacturer = 'Volvo'         AND vehicle_type ILIKE '%trailer%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 200),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  3.5)
 WHERE manufacturer = 'Hino'          AND vehicle_type ILIKE '%tronton%';

UPDATE vehicles SET fuel_tank_liters = COALESCE(fuel_tank_liters, 300),
                    fuel_ratio_kmpl  = COALESCE(fuel_ratio_kmpl,  3.2)
 WHERE manufacturer = 'Volvo'         AND vehicle_type ILIKE '%tronton%';
