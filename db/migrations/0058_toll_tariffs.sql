-- Toll gates and tariffs, so a journey can be priced without calling out.
--
-- WHY OWN THIS. Toll pricing came from MAPID, one call per journey returning
-- all five golongan. That works and stays current, but it puts a per-journey
-- external call in the path of every quote. These tables move it in-house.
--
-- THE HONEST RISK, stated here because the schema is shaped by it. Pricing
-- from a table needs a step MAPID did for us: working out which gates a route
-- passes, in order, and which pairs of them form a charge. Get that wrong and
-- the answer is a confident wrong number rather than a failure — which is why
-- toll_tariffs carries a verdict and the pricer refuses to answer when it
-- cannot explain every gate it found. A partial sum is worse than no sum.

-- Gates, with the coordinates that let a route be tested against them.
CREATE TABLE IF NOT EXISTS toll_gates (
  id          bigserial PRIMARY KEY,
  -- The canonical name, in the TARIFF's vocabulary. Tariffs are keyed on gate
  -- names as the pricing source reports them ("GT Serang Timur", "Bakauheni
  -- Selatan Toll Plaza"), so that is what this holds — not OSM's spelling and
  -- not the informal lower-case form the order system records.
  name        text NOT NULL,
  -- The matching key: lower-case, prefixes and parentheticals stripped.
  -- "Gerbang Tol Kanci", "GT Kanci" and "kanci" all normalise to "kanci".
  -- Stored rather than computed per query so one definition of "the same gate"
  -- exists, in the loader.
  name_norm   text NOT NULL,
  lat         double precision NOT NULL,
  lon         double precision NOT NULL,
  -- Where the position came from, since a gate matched by name to OSM is a
  -- weaker fact than one confirmed another way.
  source      text NOT NULL,
  ruas        text,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

-- One row per normalised name. A plaza is many booth lanes in OSM — Cikampek
-- Utama is seven nodes — and they are clustered to one gate before loading.
CREATE UNIQUE INDEX IF NOT EXISTS toll_gates_name_norm_key ON toll_gates (name_norm);

-- Alternative spellings, so the informal names in order records resolve.
CREATE TABLE IF NOT EXISTS toll_gate_aliases (
  gate_id   bigint NOT NULL REFERENCES toll_gates(id) ON DELETE CASCADE,
  alias     text NOT NULL,
  PRIMARY KEY (gate_id, alias)
);

-- Tariffs. One row per (entry, exit, golongan), or per (gate, golongan) for a
-- flat fee.
CREATE TABLE IF NOT EXISTS toll_tariffs (
  id         bigserial PRIMARY KEY,
  -- 'closed' is priced on the PAIR: enter at one gate, pay at another.
  -- 'flat'   is one tap at a fixed amount, with no exit — what an open or
  --          integrated system does. JORR is the example: one measured
  --          journey carried five consecutive flat charges.
  kind       text NOT NULL CHECK (kind IN ('closed', 'flat')),
  entry_gate bigint NOT NULL REFERENCES toll_gates(id),
  -- NULL exactly when kind = 'flat'. Enforced below rather than left to the
  -- loader, because a closed tariff missing its exit would price a journey it
  -- does not describe.
  exit_gate  bigint REFERENCES toll_gates(id),
  golongan   smallint NOT NULL CHECK (golongan BETWEEN 1 AND 5),
  amount_rp  bigint NOT NULL CHECK (amount_rp > 0),

  -- PROVENANCE, not decoration. Tariffs are reviewed every two years under
  -- UU 2/2022, so an undated figure goes stale silently; and a figure from one
  -- source is weaker than one two sources agree on.
  source       text NOT NULL,
  source_ref   text,
  effective_from date,
  fetched_at   timestamptz NOT NULL DEFAULT now(),

  -- 'usable' passed every consistency check; 'review' did not and MUST NOT be
  -- billed on unseen. The harvest found real upstream errors — one cell priced
  -- Golongan I above Golongan II, another differed 10% by direction in classes
  -- II-V while I matched exactly — so this column is the difference between
  -- catching that and shipping it.
  verdict    text NOT NULL DEFAULT 'review' CHECK (verdict IN ('usable', 'review')),
  issues     text,

  created_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT toll_tariffs_exit_matches_kind CHECK (
    (kind = 'flat'   AND exit_gate IS NULL) OR
    (kind = 'closed' AND exit_gate IS NOT NULL)
  )
);

-- One tariff per cell per class per source. Two sources are wanted, so the
-- source is part of the key: agreement between them is the thing that makes a
-- figure trustworthy, and collapsing them would destroy the evidence.
CREATE UNIQUE INDEX IF NOT EXISTS toll_tariffs_cell_key
  ON toll_tariffs (entry_gate, COALESCE(exit_gate, 0), golongan, source);

CREATE INDEX IF NOT EXISTS toll_tariffs_entry_idx ON toll_tariffs (entry_gate, golongan);

-- NOT TENANT-SCOPED, and so no RLS. A toll tariff is a published national
-- fact, the same for every operator on the platform — unlike hangars or
-- service schedules. Making it per-tenant would mean maintaining one copy per
-- company of the same government figure.
COMMENT ON TABLE toll_gates IS
  'Toll gates with positions, keyed by normalised name. Shared across tenants: a gate is a national fact.';
COMMENT ON TABLE toll_tariffs IS
  'Toll tariffs per (entry, exit, golongan). verdict=review means it failed a consistency check — do not bill on it.';
