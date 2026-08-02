package retention

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler exposes the admin restore endpoint. The sweeper itself needs no HTTP
// surface (it runs on a timer); restore is the one operator-triggered action.
type Handler struct{ s *Sweeper }

// NewHandler wraps a Sweeper for its HTTP routes.
func NewHandler(s *Sweeper) *Handler { return &Handler{s: s} }

// Routes mounts the restore endpoint — admin only.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Post("/v1/admin/restore", h.restore)
	})
}

// RestoreRequest asks to bring archived rows back into the hot DB.
type RestoreRequest struct {
	Table string `json:"table"` // "alerts" | "driving_events"
	From  string `json:"from"`  // YYYY-MM-DD (inclusive, by archive partition date)
	To    string `json:"to"`    // YYYY-MM-DD (inclusive)
}

// restore godoc
//
//	@Summary		Restore archived rows from cold storage (admin)
//	@Description	Pulls archived alerts/driving_events for a date range back into the database for the caller's tenant. Idempotent (already-present rows are skipped). Runs in the background; watch the service logs for the final counts.
//	@Tags			admin
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		retention.RestoreRequest	true	"Table and inclusive date range"
//	@Success		202		{object}	map[string]string
//	@Failure		400		{object}	map[string]string
//	@Router			/v1/admin/restore [post]
func (h *Handler) restore(w http.ResponseWriter, r *http.Request) {
	var req RestoreRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if !restorable[req.Table] {
		httpx.Error(w, http.StatusBadRequest, "table must be one of: alerts, driving_events")
		return
	}
	from, err := time.Parse("2006-01-02", req.From)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "from must be YYYY-MM-DD")
		return
	}
	to, err := time.Parse("2006-01-02", req.To)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "to must be YYYY-MM-DD")
		return
	}
	if to.Before(from) {
		httpx.Error(w, http.StatusBadRequest, "to must not be before from")
		return
	}
	if h.s.cold == nil {
		httpx.Error(w, http.StatusBadRequest, "cold storage is not configured (COLD_BUCKET unset)")
		return
	}
	tenantID := auth.TenantID(r.Context())

	// Run detached: a large restore can outlast the request/ALB idle timeout, and
	// it is idempotent, so we start it in the background and report progress to
	// the logs rather than holding the connection open.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := h.s.Restore(ctx, tenantID, req.Table, from, to); err != nil {
			log.Printf("retention: restore tenant %d %s [%s..%s]: %v", tenantID, req.Table, req.From, req.To, err)
		}
	}()

	httpx.JSON(w, http.StatusAccepted, map[string]any{
		"status": "started",
		"table":  req.Table,
		"from":   req.From,
		"to":     req.To,
		"note":   "restore runs in the background; check service logs for final counts (idempotent — safe to re-run)",
	})
}
