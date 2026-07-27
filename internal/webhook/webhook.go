// Package webhook delivers every FMS alert to a tenant-configured URL, HMAC-
// signed, with retry. Configuration lives in Settings; delivery is done by the
// background Dispatcher (dispatcher.go). The alerts table is the single source —
// camera DMS/ADAS/G-sensor events and GPS rule events both land there.
package webhook

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Endpoint is a configured webhook destination as the API returns it. The secret
// is never echoed back — HasSecret reports whether one is set.
type Endpoint struct {
	ID          int64  `json:"id"`
	URL         string `json:"url"`
	Enabled     bool   `json:"enabled"`
	EventFilter string `json:"event_filter"`
	HasSecret   bool   `json:"has_secret"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Handler struct{ pool *db.Pool }

func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/webhooks", h.list)
	r.Post("/v1/webhooks", h.create)
	r.Put("/v1/webhooks/{id}", h.update)
	r.Delete("/v1/webhooks/{id}", h.remove)
	r.Post("/v1/webhooks/{id}/test", h.test)
}

// list godoc
//
//	@Summary	Configured alarm webhooks
//	@Tags		webhooks
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[webhook.Endpoint]
//	@Router		/v1/webhooks [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	var items []Endpoint
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), `
SELECT id, url, enabled, event_filter, secret <> '',
       to_char(created_at,'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       to_char(updated_at,'YYYY-MM-DD"T"HH24:MI:SSOF:00')
FROM webhook_endpoints ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		items = []Endpoint{}
		for rows.Next() {
			var e Endpoint
			if err := rows.Scan(&e.ID, &e.URL, &e.Enabled, &e.EventFilter, &e.HasSecret, &e.CreatedAt, &e.UpdatedAt); err != nil {
				return err
			}
			items = append(items, e)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list webhooks failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[Endpoint]{Items: items, Total: int64(len(items))})
}

type upsertReq struct {
	URL         string  `json:"url"`
	Secret      *string `json:"secret"` // nil = leave unchanged on update
	Enabled     *bool   `json:"enabled"`
	EventFilter string  `json:"event_filter"`
}

// create godoc
//
//	@Summary	Add an alarm webhook
//	@Tags		webhooks
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		webhook.upsertReq	true	"endpoint"
//	@Success	201		{object}	webhook.Endpoint
//	@Router		/v1/webhooks [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req upsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.URL = strings.TrimSpace(req.URL)
	if !validURL(req.URL) {
		httpx.Error(w, http.StatusBadRequest, "url must be an http(s) URL")
		return
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	filter := req.EventFilter
	if filter == "" {
		filter = "all"
	}
	secret := ""
	if req.Secret != nil {
		secret = *req.Secret
	}
	var id int64
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		// Start the cursor at the current max alert id so a new endpoint does not
		// replay the whole history on first tick.
		return tx.QueryRow(r.Context(), `
INSERT INTO webhook_endpoints (tenant_id, url, secret, enabled, event_filter, last_alert_id)
VALUES ($1,$2,$3,$4,$5, COALESCE((SELECT max(id) FROM alerts),0))
RETURNING id`, auth.TenantID(r.Context()), req.URL, secret, enabled, filter).Scan(&id)
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "create webhook failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, Endpoint{ID: id, URL: req.URL, Enabled: enabled, EventFilter: filter, HasSecret: secret != ""})
}

// update godoc
//
//	@Summary	Update an alarm webhook
//	@Tags		webhooks
//	@Security	BearerAuth
//	@Accept		json
//	@Param		id		path	int					true	"endpoint id"
//	@Param		body	body	webhook.upsertReq	true	"endpoint"
//	@Success	204
//	@Router		/v1/webhooks/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var req upsertReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	if req.URL != "" && !validURL(strings.TrimSpace(req.URL)) {
		httpx.Error(w, http.StatusBadRequest, "url must be an http(s) URL")
		return
	}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		// COALESCE so omitted fields keep their value; secret only changes when
		// a non-nil secret is sent (nil = leave as-is).
		var secret *string
		if req.Secret != nil {
			secret = req.Secret
		}
		_, err := tx.Exec(r.Context(), `
UPDATE webhook_endpoints SET
  url          = COALESCE(NULLIF($2,''), url),
  enabled      = COALESCE($3, enabled),
  event_filter = COALESCE(NULLIF($4,''), event_filter),
  secret       = COALESCE($5, secret),
  updated_at   = now()
WHERE id = $1`, id, strings.TrimSpace(req.URL), req.Enabled, req.EventFilter, secret)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "update webhook failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// remove godoc
//
//	@Summary	Delete an alarm webhook
//	@Tags		webhooks
//	@Security	BearerAuth
//	@Param		id	path	int	true	"endpoint id"
//	@Success	204
//	@Router		/v1/webhooks/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		_, err := tx.Exec(r.Context(), `DELETE FROM webhook_endpoints WHERE id = $1`, id)
		return err
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "delete webhook failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// test godoc
//
//	@Summary	Send a test event to a webhook
//	@Tags		webhooks
//	@Security	BearerAuth
//	@Param		id	path	int	true	"endpoint id"
//	@Success	200	{object}	map[string]any
//	@Router		/v1/webhooks/{id}/test [post]
func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	var url, secret string
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT url, secret FROM webhook_endpoints WHERE id=$1`, id).Scan(&url, &secret)
	})
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "webhook not found")
		return
	}
	body := map[string]any{"type": "test", "message": "FMS webhook test", "webhook_id": id}
	status, derr := deliver(r.Context(), url, secret, body)
	resp := map[string]any{"delivered": derr == nil, "status": status}
	if derr != nil {
		resp["error"] = derr.Error()
	}
	httpx.JSON(w, http.StatusOK, resp)
}

func validURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}
