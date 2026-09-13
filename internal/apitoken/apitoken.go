// Package apitoken manages third-party API tokens (Settings > API Service).
package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/authctx"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/platform/iamclient"
	"github.com/karlo/fms-backend/internal/store"
)

// DTO is a token's metadata (never the secret, except once on create).
type DTO struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	TokenPrefix string     `json:"token_prefix"`
	Revoked     bool       `json:"revoked"`
	LastUsedAt  *time.Time `json:"last_used_at"`
	CreatedAt   *time.Time `json:"created_at"`
	// Token is populated ONLY in the create response (shown once).
	Token string `json:"token,omitempty"`
}

// CreateRequest is the create-token body.
type CreateRequest struct {
	Name string `json:"name"`
}

func fromRow(t store.ApiToken) DTO {
	return DTO{ID: t.ID, Name: t.Name, TokenPrefix: t.TokenPrefix, Revoked: t.RevokedAt.Valid, LastUsedAt: conv.Time(t.LastUsedAt), CreatedAt: conv.Time(t.CreatedAt)}
}

// Handler exposes API-token endpoints (admin only).
type Handler struct {
	pool *db.Pool
	// iam, when set, is where API keys are checked. Nil leaves the local
	// api_tokens table in use — the state before the cutover.
	iam *iamclient.Client
}

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// WithIAM points key validation at the shared authentication service.
func (h *Handler) WithIAM(c *iamclient.Client) *Handler { h.iam = c; return h }

// Routes mounts token endpoints — all require admin.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Get("/v1/api-tokens", h.list)
		ar.Post("/v1/api-tokens", h.create)
		ar.Delete("/v1/api-tokens/{id}", h.revoke)
	})
}

// Authenticator authenticates a request with a per-tenant API token instead of a
// user JWT, for machine-to-machine callers (e.g. the TMS). The token is read
// from `X-Api-Key` or `Authorization: Bearer fms_...`, hashed, and resolved to
// its tenant; the tenant is injected as read-scoped Claims so downstream handlers
// and RLS behave exactly as for a logged-in user of that account. The lookup runs
// unscoped (SystemTx) because the tenant isn't known until the token resolves —
// api_tokens uses the system-permitting RLS policy so this read is allowed.
func (h *Handler) Authenticator() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("X-Api-Key")
			if raw == "" {
				if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
					raw = strings.TrimPrefix(a, "Bearer ")
				}
			}
			raw = strings.TrimSpace(raw)
			if !strings.HasPrefix(raw, "fms_") {
				httpx.Error(w, http.StatusUnauthorized, "missing or malformed API token")
				return
			}
			// The shared authentication service holds the key when one is
			// configured. It is authoritative there — the importer copied every
			// key's hash into karlo_auth.api_keys pointed at a read-only role —
			// so checking the local table as well would let a key REVOKED
			// upstream keep working here for as long as the old row survives.
			//
			// A key resolves to a principal exactly as a user token does, which
			// is why this ends with the same auth.Claims: one way to establish
			// a caller, not two, and the permission middleware downstream does
			// not care which produced it.
			if h.iam != nil {
				p, err := h.iam.ValidateToken(r.Context(), raw)
				if err != nil {
					httpx.Error(w, http.StatusUnauthorized, "invalid or revoked API token")
					return
				}
				access := p.Access[authctx.ProductFMS]
				claims := &auth.Claims{
					TenantID: p.FMSTenantID,
					Role:     auth.RoleOperator,
					// Carried so the permission middleware answers from the key's
					// role rather than falling back to a local table that holds
					// nothing for a machine credential.
					Perms:     append([]string{}, access.Permissions...),
					GrantsAll: access.GrantsAll || p.IsPlatformStaff,
				}
				next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
				return
			}

			sum := sha256.Sum256([]byte(raw))

			var tid, id int64
			err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
				return tx.QueryRow(r.Context(),
					`SELECT id, tenant_id FROM api_tokens WHERE token_hash = $1 AND revoked_at IS NULL`,
					sum[:]).Scan(&id, &tid)
			})
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or revoked API token")
				return
			}
			// Best-effort last-used stamp; never blocks the request.
			_ = db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
				_, e := tx.Exec(r.Context(), `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, id)
				return e
			})

			claims := &auth.Claims{TenantID: tid, Role: auth.RoleOperator}
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
}

// list godoc
//
//	@Summary	List API tokens (admin)
//	@Tags		api-tokens
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{object}	httpx.List[apitoken.DTO]
//	@Router		/v1/api-tokens [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	p := httpx.ParseList(r)
	var items []DTO
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := store.New(tx).ListApiTokens(r.Context(), store.ListApiTokensParams{Lim: int32(p.Limit), Off: int32(p.Offset)})
		if err != nil {
			return err
		}
		items = make([]DTO, 0, len(rows))
		for _, t := range rows {
			items = append(items, fromRow(t))
		}
		return nil
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: int64(len(items))})
}

// create godoc
//
//	@Summary	Create an API token (admin) — the secret is returned once
//	@Tags		api-tokens
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		apitoken.CreateRequest	true	"token"
//	@Success	201		{object}	apitoken.DTO
//	@Router		/v1/api-tokens [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil || req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	secret := "fms_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(secret))
	prefix := secret[:12]

	tid := auth.TenantID(r.Context())
	uid := auth.UserID(r.Context())
	var dto DTO
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		t, err := store.New(tx).CreateApiToken(r.Context(), store.CreateApiTokenParams{
			TenantID: tid, Name: req.Name, TokenPrefix: prefix, TokenHash: sum[:], CreatedBy: &uid,
		})
		if err != nil {
			return err
		}
		dto = fromRow(t)
		dto.Token = secret // shown once
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusBadRequest, e.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, dto)
}

// revoke godoc
//
//	@Summary	Revoke an API token (admin)
//	@Tags		api-tokens
//	@Security	BearerAuth
//	@Param		id	path	int	true	"id"
//	@Success	204
//	@Router		/v1/api-tokens/{id} [delete]
func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	e := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		return store.New(tx).RevokeApiToken(r.Context(), id)
	})
	if e != nil {
		httpx.Error(w, http.StatusInternalServerError, e.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
