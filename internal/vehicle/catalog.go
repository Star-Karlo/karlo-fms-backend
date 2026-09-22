package vehicle

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
)

// The vehicle form's pickers: master data's brand / truck body / truck head
// catalogues as the caller's company sees them (Karlo's globals plus the
// company's own entries), and "+ add" for the two a company may extend.

// CatalogEntry is one picker option.
type CatalogEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Own is true for the company's private entry (as opposed to Karlo's).
	Own bool `json:"own,omitempty"`
}

// catalogKinds maps the URL segment to the master-data catalogue and to
// its HTTP create slug. truck_head is Karlo-maintained: readable, not
// addable by a tenant.
var catalogKinds = map[string]struct {
	kind    mdclient.CatalogKind
	slug    string
	addable bool
}{
	"brand":      {mdclient.KindBrand, "brand", true},
	"truck_body": {mdclient.KindTruckBody, "truckBody", true},
	"truck_head": {mdclient.KindTruckHead, "truckHead", false},
}

// CatalogRoutes mounts the picker endpoints.
func (h *Handler) CatalogRoutes(r chi.Router) {
	r.Get("/v1/catalog/{kind}", h.catalogList)
	r.Group(func(mr chi.Router) {
		mr.Use(auth.RequireRole(auth.RoleManager))
		mr.Post("/v1/catalog/{kind}", h.catalogAdd)
	})
}

// catalogList godoc
//
//	@Summary		Catalogue entries for the vehicle form (brand, truck body, truck head)
//	@Description	Master data's catalogue as this account sees it: Karlo's shared entries plus the account's own. The vehicle form's manufacturer picker reads brand, box type reads truck_body, and vehicle type reads truck_head (with truck_body as the fallback vocabulary). Empty when master data is not configured — the fields then stay free text.
//	@Tags			vehicles
//	@Security		BearerAuth
//	@Produce		json
//	@Param			kind	path		string	true	"brand | truck_body | truck_head"
//	@Success		200		{array}		vehicle.CatalogEntry
//	@Failure		400		{object}	map[string]string
//	@Router			/v1/catalog/{kind} [get]
func (h *Handler) catalogList(w http.ResponseWriter, r *http.Request) {
	k, ok := catalogKinds[chi.URLParam(r, "kind")]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "kind must be brand, truck_body or truck_head")
		return
	}
	if h.svc.cat == nil || h.dir == nil {
		httpx.JSON(w, http.StatusOK, []CatalogEntry{})
		return
	}
	company, _ := h.dir.CompanyForTenant(r.Context(), auth.EffectiveTenant(r))
	items, err := h.svc.cat.Items(r.Context(), company, k.kind)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "master data unavailable: "+err.Error())
		return
	}
	out := make([]CatalogEntry, 0, len(items))
	for _, it := range items {
		if !it.Active && it.CompanyID == "" {
			continue
		}
		out = append(out, CatalogEntry{ID: it.ID, Name: it.Name, Own: it.CompanyID != ""})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// catalogAdd godoc
//
//	@Summary		Add a catalogue entry (brand or truck body) for this account
//	@Description	Creates the entry in master data as the calling user — company-scoped for a customer, shared for platform staff — so a value the fleet needs exists once for both products. truck_head is Karlo-maintained and cannot be added here. An existing entry of the same name (case-insensitive) is returned rather than duplicated.
//	@Tags			vehicles
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			kind	path		string				true	"brand | truck_body"
//	@Param			body	body		object				true	"{\"name\":\"...\"}"
//	@Success		201		{object}	vehicle.CatalogEntry
//	@Failure		400		{object}	map[string]string
//	@Failure		403		{object}	map[string]string	"kind is Karlo-maintained"
//	@Router			/v1/catalog/{kind} [post]
func (h *Handler) catalogAdd(w http.ResponseWriter, r *http.Request) {
	k, ok := catalogKinds[chi.URLParam(r, "kind")]
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "kind must be brand or truck_body")
		return
	}
	if !k.addable {
		httpx.Error(w, http.StatusForbidden, "truck heads are maintained by Karlo; ask the platform team")
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil || strings.TrimSpace(body.Name) == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	name := strings.TrimSpace(body.Name)
	if h.svc.md == nil || h.svc.cat == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	caller, ok := h.caller(w, r)
	if !ok {
		return
	}
	company, _ := h.dir.CompanyForTenant(r.Context(), auth.EffectiveTenant(r))
	// Already there (Karlo's or ours)? Answer with it; master data would
	// refuse the duplicate anyway.
	if id := h.svc.cat.Resolve(r.Context(), company, k.kind, name); id != "" {
		httpx.JSON(w, http.StatusOK, CatalogEntry{ID: id, Name: h.svc.cat.Name(r.Context(), company, k.kind, id)})
		return
	}
	it, err := h.svc.md.CreateCatalogItem(r.Context(), caller, k.slug, name)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "master data refused the entry: "+err.Error())
		return
	}
	h.svc.cat.Invalidate(company, k.kind)
	if it.Name == "" {
		it.Name = name
	}
	httpx.JSON(w, http.StatusCreated, CatalogEntry{ID: it.ID, Name: it.Name, Own: it.CompanyID != ""})
}
