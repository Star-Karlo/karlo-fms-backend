// Package catalog resolves master data's brand / truck-body / truck-head
// catalogues for FMS, in both directions.
//
// FMS stores these as NAMES (vehicles.manufacturer, box_type, vehicle_type)
// and keeps doing so: every screen, the CSV import and the API-token writers
// send and read text. Master data stores ids. The translation lives here,
// once, at the two edges — the write-through resolves a name to an id, the
// projection turns an id back into a name — with one cached catalogue read
// per company per kind rather than a lookup per vehicle.
//
// Matching is by normalised name: case-insensitive, punctuation and spaces
// dropped, so "Mitsubishi Fuso", "mitsubishi-fuso" and "MITSUBISHI FUSO" are
// one entry. Company-scoped entries win over globals of the same name.
package catalog

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/karlo/fms-backend/internal/platform/mdclient"
)

// TTL bounds how stale a company's catalogue may be. A "+ add" from the
// form invalidates the kind it touched, so the added entry resolves at once.
const TTL = 5 * time.Minute

// Lister is the master-data read the resolver needs (mdclient.Client).
type Lister interface {
	ListCatalogItems(ctx context.Context, kind mdclient.CatalogKind, companyID string) ([]mdclient.CatalogItem, error)
}

// Resolver caches catalogues per (company, kind).
type Resolver struct {
	md Lister
	mu sync.Mutex
	c  map[key]entry
}

type key struct {
	company string
	kind    mdclient.CatalogKind
}

type entry struct {
	items  []mdclient.CatalogItem
	byNorm map[string]string // normalised name -> id
	byID   map[string]string // id -> name
	until  time.Time
}

// New returns nil when there is no master-data client, which callers treat
// as "no catalogue": names stay free text and nothing is sent upstream.
func New(md Lister) *Resolver {
	if md == nil {
		return nil
	}
	return &Resolver{md: md, c: map[key]entry{}}
}

// Normalise is the matching key: lower-case letters and digits only.
func Normalise(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func (r *Resolver) load(ctx context.Context, company string, kind mdclient.CatalogKind) (entry, error) {
	k := key{company, kind}
	r.mu.Lock()
	e, ok := r.c[k]
	r.mu.Unlock()
	if ok && time.Now().Before(e.until) {
		return e, nil
	}
	items, err := r.md.ListCatalogItems(ctx, kind, company)
	if err != nil {
		if ok {
			return e, nil // stale beats nothing
		}
		return entry{}, err
	}
	e = entry{items: items, byNorm: map[string]string{}, byID: map[string]string{}, until: time.Now().Add(TTL)}
	// Globals first, then the company's own, so a company entry of the same
	// name overwrites the global in byNorm.
	for pass := 0; pass < 2; pass++ {
		for _, it := range items {
			if (it.CompanyID == "") != (pass == 0) {
				continue
			}
			e.byID[it.ID] = it.Name
			if it.Active || it.CompanyID != "" {
				e.byNorm[Normalise(it.Name)] = it.ID
			}
		}
	}
	r.mu.Lock()
	r.c[k] = e
	r.mu.Unlock()
	return e, nil
}

// Items lists a company's view of a catalogue (globals + its own).
func (r *Resolver) Items(ctx context.Context, company string, kind mdclient.CatalogKind) ([]mdclient.CatalogItem, error) {
	e, err := r.load(ctx, company, kind)
	if err != nil {
		return nil, err
	}
	return e.items, nil
}

// Resolve returns the id for a name, or "" when the catalogue has no such
// entry (or is unreachable — a write-through then simply omits the field).
func (r *Resolver) Resolve(ctx context.Context, company string, kind mdclient.CatalogKind, name string) string {
	if r == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	e, err := r.load(ctx, company, kind)
	if err != nil {
		return ""
	}
	return e.byNorm[Normalise(name)]
}

// Name returns the display name for an id, or "" when unknown.
func (r *Resolver) Name(ctx context.Context, company string, kind mdclient.CatalogKind, id string) string {
	if r == nil || id == "" {
		return ""
	}
	e, err := r.load(ctx, company, kind)
	if err != nil {
		return ""
	}
	return e.byID[id]
}

// Invalidate drops a company's cached catalogue of one kind.
func (r *Resolver) Invalidate(company string, kind mdclient.CatalogKind) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.c, key{company, kind})
	r.mu.Unlock()
}
