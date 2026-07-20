// Package prefs stores per-user UI preferences (trip-view defaults, sensor
// tile layout, …) as an opaque jsonb blob — the frontend owns the shape.
package prefs

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// maxPrefsBytes caps the blob so a bug can't balloon rows.
const maxPrefsBytes = 64 * 1024

// Handler exposes the prefs endpoints.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the endpoints (inside the authenticated group).
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/prefs", h.get)
	r.Put("/v1/prefs", h.put)
}

// get godoc
//
//	@Summary	The calling user's UI preferences (opaque JSON)
//	@Tags		prefs
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	map[string]interface{}
//	@Router		/v1/prefs [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tenantID, userID := auth.TenantID(r.Context()), auth.UserID(r.Context())
	raw := []byte("{}")
	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(r.Context(), `SELECT prefs FROM user_prefs WHERE user_id = $1`, userID).Scan(&raw)
		if err == pgx.ErrNoRows {
			return nil
		}
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// put godoc
//
//	@Summary	Replace the calling user's UI preferences (opaque JSON)
//	@Tags		prefs
//	@Security	BearerAuth
//	@Accept		json
//	@Success	204
//	@Router		/v1/prefs [put]
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxPrefsBytes+1))
	if err != nil || len(body) > maxPrefsBytes || !json.Valid(body) {
		httpx.Error(w, http.StatusBadRequest, "body must be JSON under 64KB")
		return
	}
	tenantID, userID := auth.TenantID(r.Context()), auth.UserID(r.Context())
	err = db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO user_prefs (tenant_id, user_id, prefs, updated_at)
			VALUES ($1, $2, $3, now())
			ON CONFLICT (tenant_id, user_id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()`,
			tenantID, userID, body)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
