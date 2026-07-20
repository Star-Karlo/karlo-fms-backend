// Package dashboard stores each user's customizable, drag-and-drop dashboard
// layout (react-grid-layout items + widget keys as JSON). One row per user;
// the frontend falls back to a role preset when a user has none.
package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// Handler exposes the per-user dashboard-layout endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the dashboard endpoints (each user reads/writes only their own).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/dashboard/layout", h.get)
	r.Put("/v1/dashboard/layout", h.put)
}

// LayoutResponse is the saved layout (an empty object when the user has none).
type LayoutResponse struct {
	Layout json.RawMessage `json:"layout"`
}

// get godoc
//
//	@Summary	Get the current user's dashboard layout
//	@Tags		dashboard
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	dashboard.LayoutResponse
//	@Router		/v1/dashboard/layout [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tid, uid := auth.TenantID(r.Context()), auth.UserID(r.Context())
	layout := json.RawMessage("{}")
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		raw, err := store.New(tx).GetUserDashboard(r.Context(), store.GetUserDashboardParams{TenantID: tid, UserID: uid})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no saved layout → empty, frontend uses the role preset
		}
		if err != nil {
			return err
		}
		if len(raw) > 0 {
			layout = json.RawMessage(raw)
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, LayoutResponse{Layout: layout})
}

// put godoc
//
//	@Summary	Save the current user's dashboard layout
//	@Tags		dashboard
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		dashboard.LayoutResponse	true	"layout"
//	@Success	204
//	@Router		/v1/dashboard/layout [put]
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	var req LayoutResponse
	if err := httpx.DecodeJSON(r, &req); err != nil || len(req.Layout) == 0 {
		httpx.Error(w, http.StatusBadRequest, "layout is required")
		return
	}
	tid, uid := auth.TenantID(r.Context()), auth.UserID(r.Context())
	err := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		return store.New(tx).UpsertUserDashboard(r.Context(), store.UpsertUserDashboardParams{
			TenantID: tid, UserID: uid, Layout: []byte(req.Layout),
		})
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
