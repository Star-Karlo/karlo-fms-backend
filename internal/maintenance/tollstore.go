package maintenance

import (
	"context"

	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/routing"
)

// TollStore reads the toll tables for the pricer.
//
// NOT TENANT-SCOPED, which is why these queries do not go through db.Tx with a
// tenant. A toll tariff is a published national figure, identical for every
// operator on the platform — unlike a hangar or a service schedule — so the
// tables carry no tenant_id and no RLS (see migration 0058). Running these
// inside a tenant transaction would imply a scoping that does not exist.
type TollStore struct {
	pool *db.Pool
}

// NewTollStore builds the reader.
func NewTollStore(pool *db.Pool) *TollStore { return &TollStore{pool: pool} }

// LoadTollGates returns every gate with a position.
func (s *TollStore) LoadTollGates(ctx context.Context) ([]routing.TollGate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, name_norm, lat, lon FROM toll_gates`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []routing.TollGate
	for rows.Next() {
		var g routing.TollGate
		if err := rows.Scan(&g.ID, &g.Name, &g.NameNorm, &g.Lat, &g.Lon); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// LoadTollCells returns the tariff cells, one per (entry, exit) with its
// per-golongan amounts folded together.
//
// THE VERDICT IS AGGREGATED PESSIMISTICALLY. A cell whose rows disagree —
// several sources, or one class flagged and the others not — is returned as
// 'review', which NewTollTable then excludes entirely. Taking the best verdict
// instead would let a known-bad figure into a quote because some other class
// of the same cell happened to pass.
func (s *TollStore) LoadTollCells(ctx context.Context) ([]routing.TollCell, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
		  t.kind,
		  eg.name_norm                                  AS entry_norm,
		  coalesce(xg.name_norm, '')                    AS exit_norm,
		  t.golongan,
		  t.amount_rp,
		  t.source,
		  -- Pessimistic per CELL. Migration 0065 left one row per
		  -- (entry, exit, golongan), so a pair no longer has a second
		  -- source's copy to protect: partitioning by source here would
		  -- instead split one cell's five classes into separate groups
		  -- whenever they came from different sources, and each group would
		  -- then be judged — and priced — on its own.
		  bool_and(t.verdict = 'usable') OVER (
		    PARTITION BY t.entry_gate, coalesce(t.exit_gate, 0)
		  )                                             AS cell_usable
		FROM toll_tariffs t
		JOIN toll_gates eg ON eg.id = t.entry_gate
		LEFT JOIN toll_gates xg ON xg.id = t.exit_gate
		-- One cell per pair, all five classes together. Before 0065 this was
		-- grouped per (cell, source) because a pair could hold two rows per
		-- class; it now holds exactly one, and the classes of a single cell
		-- must stay together or Cover reports a class it cannot price.
		ORDER BY entry_norm, exit_norm, t.golongan`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type key struct{ entry, exit string }
	byCell := map[key]*routing.TollCell{}
	var order []key

	for rows.Next() {
		var kind, entryNorm, exitNorm, source string
		var golongan int16
		var amount int64
		var usable bool
		if err := rows.Scan(&kind, &entryNorm, &exitNorm, &golongan, &amount, &source, &usable); err != nil {
			return nil, err
		}
		k := key{entryNorm, exitNorm}
		c, ok := byCell[k]
		if !ok {
			c = &routing.TollCell{
				EntryNorm: entryNorm,
				ExitNorm:  exitNorm,
				Flat:      kind == "flat",
				AmountRp:  map[int]int64{},
				Verdict:   "usable",
				Source:    source,
			}
			byCell[k] = c
			order = append(order, k)
		}
		c.AmountRp[int(golongan)] = amount
		if !usable {
			c.Verdict = "review"
		}
		// A cell's classes may now come from different sources. It is worth
		// no more than its weakest one, so record that rather than whichever
		// class happened to be read first.
		if routing.SourcePriority[source] < routing.SourcePriority[c.Source] {
			c.Source = source
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]routing.TollCell, 0, len(order))
	for _, k := range order {
		out = append(out, *byCell[k])
	}
	return out, nil
}

var _ routing.TollStore = (*TollStore)(nil)
