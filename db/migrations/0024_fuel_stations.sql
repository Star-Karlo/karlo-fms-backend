-- 0024_fuel_stations.sql — official refuelling points (SPBU) as shared reference data.
--
-- DELIBERATELY NOT TENANT-SCOPED. A Pertamina station on the Cipali is the same
-- station for every customer, so this carries no tenant_id and has no RLS
-- policy — exactly like `tenants` and `spatial_ref_sys`, the only other
-- non-RLS tables here. One import serves every account.
--
-- That makes the grant model the safety boundary instead of RLS. The default
-- privileges from 0005 hand fms_app INSERT/UPDATE/DELETE on every new table,
-- which is wrong for shared data: a bug in any one tenant's request path could
-- delete stations out from under all the others. So writes are revoked below
-- and the app gets SELECT only. The importer (cmd/import-fuel) connects as the
-- owner, the same role migrations run as.
--
-- Source is OpenStreetMap (amenity=fuel). ODbL requires attribution wherever
-- the data is shown — the map layer carries "© OpenStreetMap contributors".

CREATE TABLE IF NOT EXISTS fuel_station (
  id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,

  -- OSM element identity: type ('node'|'way') + id. Together these are the
  -- upsert key, so re-running the import updates in place rather than
  -- duplicating every station on every run. A station mapped as a building
  -- footprint is a `way`; one mapped as a single point is a `node`, and the two
  -- id spaces overlap — hence the type is part of the key, not decoration.
  osm_type   text   NOT NULL CHECK (osm_type IN ('node', 'way', 'relation')),
  osm_id     bigint NOT NULL,

  -- Normalised operator: 'pertamina' | 'shell' | 'bp' | 'vivo' | 'akr' | 'other'.
  -- OSM brand tags are free text ("SPBU Pertamina", "PERTAMINA", "Shell
  -- Indonesia"), so cmd/import-fuel folds them into this closed set and keeps
  -- the original in brand_raw. Filtering and colouring key off `brand`; the raw
  -- value is kept so a bad normalisation is diagnosable rather than lost.
  brand      text   NOT NULL,
  brand_raw  text,
  name       text,

  lat        double precision NOT NULL CHECK (lat BETWEEN -11 AND 6),
  lon        double precision NOT NULL CHECK (lon BETWEEN 95 AND 141),

  -- Generated, so it can never drift from lat/lon the way a trigger-maintained
  -- column can. Geography (not geometry) means ST_DWithin takes metres
  -- directly, which is what fuel-stop detection will want later.
  geog geography(Point, 4326)
    GENERATED ALWAYS AS (ST_SetSRID(ST_MakePoint(lon, lat), 4326)::geography) STORED,

  -- 'jawa' | 'sumatra', assigned by the importer from the query region. Lets
  -- the API serve one island without a spatial predicate.
  island     text,

  -- Reverse-geocoded lazily; null until then. Not populated at import time
  -- because that would mean thousands of geocoder calls for data the map does
  -- not need in order to draw a pin.
  kota       text,
  provinsi   text,

  updated_at timestamptz NOT NULL DEFAULT now(),

  UNIQUE (osm_type, osm_id)
);

-- The map always queries by viewport, so the spatial index is the one that
-- matters. GiST over geography handles both ST_DWithin and bbox intersection.
CREATE INDEX IF NOT EXISTS idx_fuel_station_geog  ON fuel_station USING GIST (geog);
CREATE INDEX IF NOT EXISTS idx_fuel_station_brand ON fuel_station (brand);

-- Read-only for the application role. See the header: this table is shared
-- across tenants, so the app must not be able to mutate it.
REVOKE INSERT, UPDATE, DELETE ON fuel_station FROM fms_app;
GRANT SELECT ON fuel_station TO fms_app;
