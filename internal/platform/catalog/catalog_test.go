package catalog

import (
	"context"
	"testing"

	"github.com/karlo/fms-backend/internal/platform/mdclient"
)

type fakeLister struct {
	items []mdclient.CatalogItem
	calls int
}

func (f *fakeLister) ListCatalogItems(_ context.Context, _ mdclient.CatalogKind, _ string) ([]mdclient.CatalogItem, error) {
	f.calls++
	return f.items, nil
}

// Names match ignoring case, spaces and punctuation; a company's own entry
// wins over Karlo's of the same name; one read serves many lookups.
func TestResolveNormalisesAndPrefersCompanyEntries(t *testing.T) {
	l := &fakeLister{items: []mdclient.CatalogItem{
		{ID: "g1", Name: "Mitsubishi Fuso", Active: true},
		{ID: "g2", Name: "Hino", Active: false},
		{ID: "c1", Name: "HINO", CompanyID: "co"},
	}}
	r := New(l)
	ctx := context.Background()
	if got := r.Resolve(ctx, "co", mdclient.KindBrand, " mitsubishi-fuso "); got != "g1" {
		t.Errorf("normalised match: got %q, want g1", got)
	}
	if got := r.Resolve(ctx, "co", mdclient.KindBrand, "hino"); got != "c1" {
		t.Errorf("company entry should win over an (inactive) global: got %q", got)
	}
	if got := r.Resolve(ctx, "co", mdclient.KindBrand, "Scania"); got != "" {
		t.Errorf("unknown name resolved to %q", got)
	}
	if got := r.Name(ctx, "co", mdclient.KindBrand, "g1"); got != "Mitsubishi Fuso" {
		t.Errorf("Name(g1) = %q", got)
	}
	if l.calls != 1 {
		t.Errorf("catalogue read %d times; want 1 (cached)", l.calls)
	}
	r.Invalidate("co", mdclient.KindBrand)
	r.Resolve(ctx, "co", mdclient.KindBrand, "hino")
	if l.calls != 2 {
		t.Errorf("after Invalidate the catalogue should be re-read; reads = %d", l.calls)
	}
	var nilR *Resolver
	if nilR.Resolve(ctx, "co", mdclient.KindBrand, "Hino") != "" {
		t.Error("nil resolver must resolve nothing")
	}
}
