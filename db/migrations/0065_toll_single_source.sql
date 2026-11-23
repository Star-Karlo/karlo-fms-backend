-- One tariff per cell: collapse the per-source rows to a single list.
--
-- WHY. toll_tariffs was keyed on (entry, exit, golongan, SOURCE), so one
-- charge could hold two rows — MAPID's and Jasa Marga's — and the pricer chose
-- between them at query time. On the live data the two sources price 220 of
-- the same cells and disagree on 179 of them (81%), worst case Rp 24.500 on
-- weleri -> kandeman, so that choice was being made constantly and invisibly.
--
-- From here there is ONE LIST. A cell is one row. `source` survives only as a
-- label on that row saying where the figure came from; it is no longer part of
-- the key and no longer decides anything at query time.
--
-- PRICES DO NOT MOVE. The surviving row is picked with exactly the rule
-- NewTollTable already applied — a usable row beats a review row, then the
-- better source wins — so every quote returns what it returned yesterday.
--
-- HOW TO GET THE DISCARDED ROWS BACK. Re-run the loads that produced them:
-- 0059 and 0061 (MAPID) and 0063 (Jasa Marga) are in this repo and are
-- idempotent. That is the recovery path, which is why this deletes rather than
-- archiving to a side table nobody would ever read.

BEGIN;

-- The ranking, in SQL, so the collapse below and the importer's upsert agree
-- on it. It MUST stay in step with routing.SourcePriority in Go; a test reads
-- this file and asserts that it does.
--
-- It still matters after the collapse, not for choosing between two stored
-- rows — there are none — but for deciding whether an INCOMING figure may
-- replace the stored one. Without it, re-running the MAPID harvest would
-- overwrite Jasa Marga's published tariffs on all 179 cells where they differ.
--
-- 'manual' sits above the harvests and below a decree: a person who has looked
-- at the gate outranks a scraped figure, but not the legal one.
CREATE OR REPLACE FUNCTION toll_source_rank(src text) RETURNS int
  LANGUAGE sql IMMUTABLE PARALLEL SAFE AS $$
  SELECT CASE src
    WHEN 'decree'    THEN 30
    WHEN 'manual'    THEN 25
    WHEN 'jasamarga' THEN 20
    WHEN 'mapid'     THEN 10
    ELSE 0
  END
$$;

WITH ranked AS (
  SELECT id,
         row_number() OVER (
           PARTITION BY entry_gate, COALESCE(exit_gate, 0), golongan
           -- Mirrors NewTollTable: a review row is not a pricing candidate,
           -- then the better source wins. id last so this is deterministic.
           ORDER BY (verdict = 'usable') DESC, toll_source_rank(source) DESC, id
         ) AS rn
  FROM toll_tariffs
)
DELETE FROM toll_tariffs t
USING ranked r
WHERE r.id = t.id AND r.rn > 1;

-- The key loses its source dimension, which is what makes one row per cell an
-- invariant the database enforces rather than a convention the importer keeps.
DROP INDEX IF EXISTS toll_tariffs_cell_key;

CREATE UNIQUE INDEX IF NOT EXISTS toll_tariffs_cell_key
  ON toll_tariffs (entry_gate, COALESCE(exit_gate, 0), golongan);

COMMENT ON TABLE toll_tariffs IS
  'Toll tariffs, one row per (entry, exit, golongan). source labels where that figure came from. verdict=review means it failed a consistency check — do not bill on it.';

COMMIT;
