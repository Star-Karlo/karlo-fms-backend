// Package license lists vehicle & driver documents (STNK, KIR, SIM, insurance)
// with computed expiry-status buckets. Backed by the shared `documents` table
// (STNK/KIR were imported from the legacy fleet export).
package license

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is a document row with its computed status bucket.
type DTO struct {
	ID           int64   `json:"id"`
	OwnerType    string  `json:"owner_type"`
	OwnerID      int64   `json:"owner_id"`
	VehiclePlate *string `json:"vehicle_plate"`
	DriverName   *string `json:"driver_name"`
	Kind         *string `json:"kind"`
	Number       *string `json:"number"`
	Expiry       *string `json:"expiry"`
	Status       string  `json:"status"` // expired | will_expire | valid | unidentified
}

// Counts is the status-bucket summary.
type Counts struct {
	Expired      int64 `json:"expired"`
	WillExpire   int64 `json:"will_expire"`
	Valid        int64 `json:"valid"`
	Unidentified int64 `json:"unidentified"`
}

// Response bundles a page of documents with the bucket counts.
type Response struct {
	Items  []DTO  `json:"items"`
	Total  int64  `json:"total"`
	Counts Counts `json:"counts"`
}

// Handler exposes document/license endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts license endpoints.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/documents", h.list)
}

func strPtrParam(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// list godoc
//
//	@Summary	List documents with expiry status (License module)
//	@Tags		license
//	@Security	BearerAuth
//	@Produce	json
//	@Param		owner_type	query		string	false	"vehicle|driver"
//	@Param		status		query		string	false	"expired|will_expire|valid|unidentified"
//	@Param		q			query		string	false	"search number/kind/plate"
//	@Success	200	{object}	license.Response
//	@Router		/v1/documents [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	q := r.URL.Query()
	ownerType := strPtrParam(q.Get("owner_type"))
	status := strPtrParam(q.Get("status"))
	tid := auth.TenantID(r.Context())

	var resp Response
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		st := store.New(tx)
		rows, err := st.ListDocuments(r.Context(), store.ListDocumentsParams{
			OwnerType: ownerType, Status: status, Q: conv.StrPtr(p.Q),
			Lim: int32(p.Limit), Off: int32(p.Offset),
		})
		if err != nil {
			return err
		}
		resp.Items = make([]DTO, 0, len(rows))
		for _, d := range rows {
			resp.Items = append(resp.Items, DTO{
				ID: d.ID, OwnerType: d.OwnerType, OwnerID: d.OwnerID, VehiclePlate: d.VehiclePlate,
				DriverName: d.DriverName, Kind: d.Kind, Number: d.Number,
				Expiry: conv.DateStr(d.Expiry), Status: d.DocStatus,
			})
		}
		resp.Total, err = st.CountDocuments(r.Context(), store.CountDocumentsParams{OwnerType: ownerType, Status: status, Q: conv.StrPtr(p.Q)})
		if err != nil {
			return err
		}
		c, err := st.DocumentStatusCounts(r.Context(), ownerType)
		if err != nil {
			return err
		}
		resp.Counts = Counts{Expired: c.Expired, WillExpire: c.WillExpire, Valid: c.Valid, Unidentified: c.Unidentified}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, resp)
}
