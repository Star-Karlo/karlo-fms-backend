// Package apitoken manages third-party API tokens (Settings > API Service).
package apitoken

import (
	"context"
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
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	Revoked     bool   `json:"revoked"`
	// IssuedHere distinguishes a key FMS minted and verifies itself from one
	// the shared auth service owns. It decides where revoking actually has to
	// happen, so the list can say so instead of letting someone revoke a copy
	// and wonder why the key still works.
	IssuedHere bool       `json:"issued_here"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  *time.Time `json:"created_at"`
	// Token is populated ONLY in the create response (shown once).
	Token string `json:"token,omitempty"`
}

// CreateRequest is the create-token body.
type CreateRequest struct {
	Name string `json:"name"`
}

func fromRow(t store.ApiToken) DTO {
	return DTO{
		ID: t.ID, Name: t.Name, TokenPrefix: t.TokenPrefix,
		Revoked: t.RevokedAt.Valid, IssuedHere: t.LocalAuth,
		LastUsedAt: conv.Time(t.LastUsedAt), CreatedAt: conv.Time(t.CreatedAt),
	}
}

// Handler exposes API-token endpoints (admin only).
type Handler struct {
	pool *db.Pool
	// iam, when set, is where API keys are checked. Nil leaves the local
	// api_tokens table in use — the state before the cutover.
	iam *iamclient.Client
	// dir, when set, answers whether a tenant's company still has API access.
	//
	// Needed for LOCAL keys only. A key held in karlo_auth already has the
	// switch applied upstream — validateAPIKey checks it before resolving
	// anything — but a key FMS issued itself is unknown there, so without this
	// nothing on its path ever consults the grant. That was the two products
	// disagreeing: a link cut in Karlo admin stopped every upstream key and
	// left every FMS-issued one working.
	dir APIGrantResolver
}

// APIGrantResolver answers whether a tenant's company still has API access.
//
// A one-method interface, matching auth.TenantResolver: this package needs one
// question answered and should not depend on the directory that answers it.
// The second return says whether the question could be ANSWERED at all, which
// the caller must not collapse into "no" — see localClaims.
type APIGrantResolver interface {
	APIEnabledForTenant(ctx context.Context, tenantID int64) (enabled bool, known bool)
}

// grantWithdrawn reports whether this tenant's company has had its API access
// taken away. Three cases, and only the first refuses:
//
//   - known and disabled — the switch is off, so refuse;
//   - known and enabled — allow;
//   - NOT KNOWN — allow, and this is the one that matters. It means the
//     directory could not answer: unreachable, or the tenant is not an FMS
//     company upstream. Reading it as "forbidden" would stop every
//     integration in the fleet during an outage of a service that is not even
//     in the request path, and would refuse a local key that resolves against
//     a real row in this database.
//
// A nil resolver is a deployment that has not cut over; local keys then behave
// exactly as they did before.
func grantWithdrawn(ctx context.Context, g APIGrantResolver, tenantID int64) bool {
	if g == nil {
		return false
	}
	enabled, known := g.APIEnabledForTenant(ctx, tenantID)
	return known && !enabled
}

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// WithIAM points key validation at the shared authentication service.
func (h *Handler) WithIAM(c *iamclient.Client) *Handler { h.iam = c; return h }

// WithDirectory supplies the tenant directory, so a locally issued key can be
// checked against its company's API grant. Without it local keys behave as
// they did before: resolved on this database alone.
func (h *Handler) WithDirectory(d APIGrantResolver) *Handler { h.dir = d; return h }

// Routes mounts token endpoints — all require admin.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RoleAdmin))
		ar.Get("/v1/api-tokens", h.list)
		ar.Post("/v1/api-tokens", h.create)
		ar.Delete("/v1/api-tokens/{id}", h.remove)
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
	return h.AuthenticatorOr(nil)
}

// AuthenticatorOr is the same, but hands a request that is NOT carrying an API
// token to another authenticator — in practice the user JWT one.
//
// Integration endpoints are reached two ways and both are legitimate: a
// machine presenting its account's key, and a signed-in person trying the
// same call from the API docs page. Without this they are mutually exclusive,
// and the symptom is the confusing one — a key minted a minute ago answering
// "invalid or expired token", because the request never reached the API-token
// middleware at all.
func (h *Handler) AuthenticatorOr(fallback func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		var fell http.Handler
		if fallback != nil {
			fell = fallback(next)
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("X-Api-Key")
			if raw == "" {
				if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
					raw = strings.TrimPrefix(a, "Bearer ")
				}
			}
			raw = strings.TrimSpace(raw)
			if !strings.HasPrefix(raw, "fms_") {
				if fell != nil {
					fell.ServeHTTP(w, r) // not a key — let the JWT path judge it
					return
				}
				httpx.Error(w, http.StatusUnauthorized, "missing or malformed API token")
				return
			}
			// Two stores, and each key belongs to exactly one of them.
			//
			// The shared authentication service owns the keys the importer
			// copied into karlo_auth.api_keys at the cutover. It is
			// authoritative for those, and a local row is only a copy — which
			// is why FindLocalApiToken cannot see them: matching the stale
			// copy would let a key REVOKED upstream keep working here.
			//
			// FMS owns the keys it issues itself (local_auth, see 0054).
			// Those are unknown upstream, so asking there alone meant every
			// key minted in FMS settings since the cutover was refused the
			// moment it was used — and refused with "revoked", which reads
			// like somebody cancelled it rather than it never having been
			// registered anywhere.
			//
			// Either way a key resolves to the same auth.Claims a user token
			// produces: one way to establish a caller, not two, and the
			// permission middleware downstream does not care which made it.
			if h.iam != nil {
				p, err := h.iam.ValidateToken(r.Context(), raw)
				if err != nil {
					if claims, ok := h.localClaims(r, raw); ok {
						next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
						return
					}
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

			claims, ok := h.localClaims(r, raw)
			if !ok {
				httpx.Error(w, http.StatusUnauthorized, "invalid or revoked API token")
				return
			}
			next.ServeHTTP(w, r.WithContext(auth.WithClaims(r.Context(), claims)))
		})
	}
}

// localClaims resolves a key FMS issued itself. The lookup runs unscoped
// (SystemTx) because the tenant is not known until the key resolves —
// api_tokens uses the system-permitting RLS policy so this read is allowed.
func (h *Handler) localClaims(r *http.Request, raw string) (*auth.Claims, bool) {
	sum := sha256.Sum256([]byte(raw))
	var row store.FindLocalApiTokenRow
	if err := db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		var e error
		row, e = store.New(tx).FindLocalApiToken(r.Context(), sum[:])
		return e
	}); err != nil {
		return nil, false
	}
	// Best-effort last-used stamp; never blocks the request.
	_ = db.SystemTx(r.Context(), h.pool, func(tx pgx.Tx) error {
		_, e := tx.Exec(r.Context(), `UPDATE api_tokens SET last_used_at = now() WHERE id = $1`, row.ID)
		return e
	})
	// The company-wide grant, applied to a key karlo_auth has never heard of.
	//
	// Deliberately AFTER the row is found, so the refusal distinguishes a key
	// that does not exist from one whose company's access was withdrawn — the
	// same distinction ErrAPIDisabled draws upstream, and the reason an
	// integrator is not sent hunting a credential problem that is not theirs.
	//
	// `known` false means the directory could not answer: either it is
	// unreachable or this tenant is not an FMS company upstream. Neither is
	// grounds to refuse a key that resolves against a real local row — an
	// outage of a service outside the request path must not stop every
	// integration in the fleet.
	if grantWithdrawn(r.Context(), h.dir, row.TenantID) {
		return nil, false
	}

	return &auth.Claims{TenantID: row.TenantID, Role: auth.RoleOperator}, true
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
	var dto DTO
	e := db.Tx(r.Context(), h.pool, tid, func(tx pgx.Tx) error {
		// created_by is a foreign key into THIS database's users table, and
		// the signed-in admin may not be in it: an SSO session carries the
		// platform's user id from karlo_auth, which has no local row. Filling
		// it blindly made every token creation by a platform or TMS admin fail
		// with a foreign-key violation — the credential could not be issued at
		// all, and the error named a constraint rather than the problem.
		//
		// Attribution is worth having and not worth blocking on, so it is
		// recorded when the user is local and left null when they are not.
		var createdBy *int64
		if uid := auth.UserID(r.Context()); uid > 0 {
			var exists bool
			if err := tx.QueryRow(r.Context(),
				`SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, uid).Scan(&exists); err == nil && exists {
				createdBy = &uid
			}
		}
		t, err := store.New(tx).CreateApiToken(r.Context(), store.CreateApiTokenParams{
			TenantID: tid, Name: req.Name, TokenPrefix: prefix, TokenHash: sum[:], CreatedBy: createdBy,
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

// RemoveResult says which of the two steps ran.
type RemoveResult struct {
	// Revoked is the first step: the key stops working, the row stays so the
	// list still shows what it was and when it was last used.
	Revoked bool `json:"revoked"`
	// Deleted is the second: the row goes too.
	Deleted bool   `json:"deleted"`
	Name    string `json:"name"`
}

// remove godoc
//
//	@Summary		Revoke an API token, and delete it on the second call (admin)
//	@Description	Two steps, like deleting a vehicle. On a live token this REVOKES it: the key stops working immediately, and the row stays so the list can still say what it was called and when it was last used — which is how you find out whether anything was still calling with it. Called again on a token that is already revoked, it DELETES the row. The answer says which step ran. A key owned by the shared auth service can be deleted from this list, but revoking it there is what stops it working.
//	@Tags			api-tokens
//	@Security		BearerAuth
//	@Produce		json
//	@Param			id	path		int	true	"id"
//	@Success		200	{object}	apitoken.RemoveResult
//	@Failure		404	{object}	map[string]string
//	@Router			/v1/api-tokens/{id} [delete]
func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var out RemoveResult
	e := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetApiToken(r.Context(), id)
		if err != nil {
			return err
		}
		out.Name = before.Name
		if !before.RevokedAt.Valid {
			out.Revoked = true
			return q.RevokeApiToken(r.Context(), id)
		}
		n, err := q.DeleteApiToken(r.Context(), id)
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		out.Deleted = true
		return nil
	})
	if e != nil {
		httpx.Error(w, http.StatusNotFound, "token not found")
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
