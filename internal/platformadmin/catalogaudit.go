package platformadmin

import (
	"context"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/catalog"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/mdclient"
	"github.com/karlo/fms-backend/internal/platform/mdwrite"
)

// One-off: FMS's free-text manufacturer / vehicle_type / box_type against
// master data's catalogues, per account.
//
// Before write-through carried catalogue ids, every vehicle created or
// edited in FMS reached master data without brand/body/head, and the text
// FMS kept was invisible to TMS. The audit lists what FMS holds and how it
// matches; apply pushes the ids for the matched vehicles as the operator.
// The unmatched values are the operator's to map or add — a combined value
// like "Trailer Flatbed 40ft" is a head and a body in one, and nobody but
// a person should split it.

// CatalogAuditValue is one distinct free-text value in one column of one
// account, with its match.
type CatalogAuditValue struct {
	TenantID   int64  `json:"tenant_id"`
	TenantName string `json:"tenant_name"`
	Column     string `json:"column"` // manufacturer | vehicle_type | box_type
	Value      string `json:"value"`
	Vehicles   int    `json:"vehicles"` // rows carrying it (paired with master data)
	// Match: "brand" | "truck_body" | "truck_head" with the catalogue entry it
	// resolved to, or "none".
	Match     string `json:"match"`
	MatchID   string `json:"match_id,omitempty"`
	MatchName string `json:"match_name,omitempty"`
}

// CatalogAudit is the whole report.
type CatalogAudit struct {
	Values    []CatalogAuditValue `json:"values"`
	Matched   int                 `json:"matched"`
	Unmatched int                 `json:"unmatched"`
}

// WithCatalog supplies the resolver the audit reads through.
func (h *Handler) WithCatalog(c *catalog.Resolver) *Handler { h.cat = c; return h }

// catalogAudit godoc
//
//	@Summary		Free-text vehicle brand/type/body values vs master data's catalogues (platform admin)
//	@Description	For every account: each distinct manufacturer, vehicle_type and box_type on vehicles paired with master data, with the catalogue entry it resolves to by normalised name (manufacturer→brand; box_type→truck body; vehicle_type→truck body first, then truck head) or "none". The "none" rows are the operator's to map or add in TMS's catalogue; apply (POST) pushes the matched ones.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	platformadmin.CatalogAudit
//	@Failure		503	{object}	map[string]string	"master data not configured"
//	@Router			/v1/platform/catalog-audit [get]
func (h *Handler) catalogAudit(w http.ResponseWriter, r *http.Request) {
	if h.cat == nil || h.dir == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	rep, err := h.audit(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, rep)
}

type auditRow struct {
	tenant  tenantRef
	company string
	mdID    string
	manu    string
	vtype   string
	box     string
}

func (h *Handler) rows(ctx context.Context) ([]auditRow, error) {
	tenants, err := h.tenantList(ctx)
	if err != nil {
		return nil, err
	}
	var out []auditRow
	for _, t := range tenants {
		company, _ := h.dir.CompanyForTenant(ctx, t.id)
		if company == "" {
			continue
		}
		err := db.Tx(ctx, h.pool, t.id, func(tx pgx.Tx) error {
			rs, err := tx.Query(ctx, `
				SELECT master_data_id, coalesce(manufacturer,''), coalesce(vehicle_type,''), coalesce(box_type,'')
				  FROM vehicles
				 WHERE master_data_id IS NOT NULL AND status <> 'archive'
				   AND (nullif(manufacturer,'') IS NOT NULL OR nullif(vehicle_type,'') IS NOT NULL OR nullif(box_type,'') IS NOT NULL)`)
			if err != nil {
				return err
			}
			defer rs.Close()
			for rs.Next() {
				a := auditRow{tenant: t, company: company}
				if err := rs.Scan(&a.mdID, &a.manu, &a.vtype, &a.box); err != nil {
					return err
				}
				out = append(out, a)
			}
			return rs.Err()
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// resolve maps one column's value to a catalogue entry.
func (h *Handler) resolve(ctx context.Context, company, column, value string) (match, id, name string) {
	try := func(kind mdclient.CatalogKind, label string) bool {
		if v := h.cat.Resolve(ctx, company, kind, value); v != "" {
			match, id, name = label, v, h.cat.Name(ctx, company, kind, v)
			return true
		}
		return false
	}
	switch column {
	case "manufacturer":
		try(mdclient.KindBrand, "brand")
	case "box_type":
		try(mdclient.KindTruckBody, "truck_body")
	case "vehicle_type":
		if !try(mdclient.KindTruckBody, "truck_body") {
			try(mdclient.KindTruckHead, "truck_head")
		}
	}
	if match == "" {
		match = "none"
	}
	return
}

func (h *Handler) audit(ctx context.Context) (CatalogAudit, error) {
	rows, err := h.rows(ctx)
	if err != nil {
		return CatalogAudit{}, err
	}
	type k struct {
		tenant int64
		column string
		norm   string
	}
	agg := map[k]*CatalogAuditValue{}
	for _, a := range rows {
		for _, col := range []struct{ name, value string }{{"manufacturer", a.manu}, {"vehicle_type", a.vtype}, {"box_type", a.box}} {
			if strings.TrimSpace(col.value) == "" {
				continue
			}
			kk := k{a.tenant.id, col.name, catalog.Normalise(col.value)}
			v := agg[kk]
			if v == nil {
				m, id, name := h.resolve(ctx, a.company, col.name, col.value)
				v = &CatalogAuditValue{TenantID: a.tenant.id, TenantName: a.tenant.name, Column: col.name, Value: strings.TrimSpace(col.value), Match: m, MatchID: id, MatchName: name}
				agg[kk] = v
			}
			v.Vehicles++
		}
	}
	rep := CatalogAudit{Values: make([]CatalogAuditValue, 0, len(agg))}
	for _, v := range agg {
		rep.Values = append(rep.Values, *v)
		if v.Match == "none" {
			rep.Unmatched++
		} else {
			rep.Matched++
		}
	}
	sort.Slice(rep.Values, func(i, j int) bool {
		a, b := rep.Values[i], rep.Values[j]
		if a.TenantID != b.TenantID {
			return a.TenantID < b.TenantID
		}
		if a.Column != b.Column {
			return a.Column < b.Column
		}
		return a.Vehicles > b.Vehicles
	})
	return rep, nil
}

// CatalogApplyResult says what apply pushed.
type CatalogApplyResult struct {
	Vehicles int `json:"vehicles"` // paired vehicles with at least one resolvable value
	Updated  int `json:"updated"`  // master data accepted
	Failed   int `json:"failed"`
}

// catalogApply godoc
//
//	@Summary		Push the matched brand/body/head ids to master data (platform admin)
//	@Description	For every paired vehicle whose free-text values resolve in the audit, PUT /vehicles/{id} upstream with brandId / truckBodyId / truckHeadId, as the operator (acting for each account). Unmatched values are left alone. Idempotent — run it again after adding catalogue entries.
//	@Tags			platform
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	platformadmin.CatalogApplyResult
//	@Failure		503	{object}	map[string]string	"master data not configured"
//	@Router			/v1/platform/catalog-audit/apply [post]
func (h *Handler) catalogApply(w http.ResponseWriter, r *http.Request) {
	if h.cat == nil || h.dir == nil || h.md == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "master data is not configured")
		return
	}
	rows, err := h.rows(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	var res CatalogApplyResult
	for _, a := range rows {
		v := mdwrite.Vehicle{}
		if m, id, _ := h.resolve(r.Context(), a.company, "manufacturer", a.manu); m == "brand" {
			v.BrandID = id
		}
		if m, id, _ := h.resolve(r.Context(), a.company, "box_type", a.box); m == "truck_body" {
			v.TruckBodyID = id
		}
		switch m, id, _ := h.resolve(r.Context(), a.company, "vehicle_type", a.vtype); m {
		case "truck_head":
			v.TruckHeadID = id
		case "truck_body":
			if v.TruckBodyID == "" {
				v.TruckBodyID = id
			}
		}
		if v.BrandID == "" && v.TruckBodyID == "" && v.TruckHeadID == "" {
			continue
		}
		res.Vehicles++
		caller := mdwrite.Caller{Bearer: bearer, ActingFor: a.company}
		if err := h.md.UpdateVehicle(r.Context(), caller, a.mdID, v); err != nil {
			res.Failed++
			log.Printf("catalog apply: %s vehicle %s: %v", a.tenant.name, a.mdID, err)
			continue
		}
		res.Updated++
	}
	log.Printf("catalog apply by user %d: %d vehicles, %d updated, %d failed", auth.UserID(r.Context()), res.Vehicles, res.Updated, res.Failed)
	httpx.JSON(w, http.StatusOK, res)
}
