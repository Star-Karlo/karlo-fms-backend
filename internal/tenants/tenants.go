// Package tenants provisions customers: a tenant plus the first login that can
// administer it.
//
// WHY THIS EXISTS. Onboarding a customer used to mean connecting to the
// database as the owner and running seed/new-tenant.sql by hand. That needs the
// RDS master password and an open admin CIDR — neither of which is available
// from outside the VPC, so onboarding was effectively blocked. Worse, it is the
// kind of hand-run SQL where a mistyped tenant_id silently gives a new customer
// visibility of somebody else's fleet.
//
// THE SECURITY BOUNDARY. These endpoints are the only place in the API that
// writes ACROSS tenants, so they are restricted to RolePlatformAdmin — the
// operator of the service, not any customer's own administrator (who holds
// RoleAdmin). See internal/auth/model.go and migration 0027.
//
// HOW IT STAYS SAFE UNDER RLS. Creating a tenant is a single transaction that
// re-scopes itself: the tenant row is inserted first (the `tenants` table
// carries no RLS policy), then app.current_tenant is set to the NEW id for the
// rest of the transaction so the admin user and default fleet group are written
// under exactly the isolation every other write obeys. Notably this does NOT
// use db.SystemTx — that clears the tenant scope, and `users` has a
// login-bootstrap policy that is permissive when the tenant is unset, which
// would let a bad tenant_id land unchecked.
package tenants

import (
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// DTO is the public shape of a tenant, with a little context so the admin page
// can show whether a customer has actually been set up.
type DTO struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	Users     int64  `json:"users"`
	Vehicles  int64  `json:"vehicles"`
}

// CreateRequest provisions a customer and its first administrator.
type CreateRequest struct {
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	AdminEmail    string `json:"admin_email"`
	AdminName     string `json:"admin_name"`
	AdminPassword string `json:"admin_password"`
}

// CreateResponse reports what was provisioned.
type CreateResponse struct {
	Tenant     DTO    `json:"tenant"`
	AdminID    int64  `json:"admin_id"`
	AdminEmail string `json:"admin_email"`
}

// slugRe keeps slugs URL-safe and stable: they identify a tenant in operational
// tooling (seed scripts, imports, support queries) and are awkward to change.
var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

// minPasswordLen is deliberately modest. These are provisioning passwords meant
// to be handed over and changed, and the app already ships a change-password
// flow; refusing anything shorter would just push operators to write them down.
const minPasswordLen = 6

// Handler exposes tenant provisioning (platform admin only).
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts tenant endpoints. Everything here requires platform admin —
// this is the one part of the API that crosses the tenant boundary.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(ar chi.Router) {
		ar.Use(auth.RequireRole(auth.RolePlatformAdmin))
		ar.Get("/v1/tenants", h.list)
		ar.Post("/v1/tenants", h.create)
	})
}

// list godoc
//
//	@Summary		List tenants (platform admin)
//	@Description	Every customer on the platform, with user and vehicle counts.
//	@Tags			tenants
//	@Security		BearerAuth
//	@Produce		json
//	@Success		200	{object}	httpx.List[tenants.DTO]
//	@Failure		403	{object}	map[string]string
//	@Router			/v1/tenants [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	// Counts are read here rather than per-tenant so the page is one round trip.
	// `tenants` has no RLS policy; users/vehicles do, so they are counted with
	// explicit joins under the platform admin's own scope — which is why this
	// runs as a plain pool query rather than inside a tenant-scoped Tx.
	rows, err := h.pool.Query(r.Context(), `
SELECT t.id, t.name, t.slug, to_char(t.created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       (SELECT count(*) FROM users u WHERE u.tenant_id = t.id),
       (SELECT count(*) FROM vehicles v WHERE v.tenant_id = t.id)
FROM tenants t
ORDER BY t.id`)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "list tenants failed")
		return
	}
	defer rows.Close()

	items := []DTO{}
	for rows.Next() {
		var d DTO
		if err := rows.Scan(&d.ID, &d.Name, &d.Slug, &d.CreatedAt, &d.Users, &d.Vehicles); err != nil {
			httpx.Error(w, http.StatusInternalServerError, "list tenants failed")
			return
		}
		items = append(items, d)
	}
	if rows.Err() != nil {
		httpx.Error(w, http.StatusInternalServerError, "list tenants failed")
		return
	}
	httpx.JSON(w, http.StatusOK, httpx.List[DTO]{Items: items, Total: int64(len(items))})
}

// create godoc
//
//	@Summary		Provision a tenant and its first admin (platform admin)
//	@Description	Creates the customer, its administrator login and a default fleet group in one transaction. Replaces running seed/new-tenant.sql against the database by hand.
//	@Tags			tenants
//	@Security		BearerAuth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		tenants.CreateRequest	true	"Tenant and first administrator"
//	@Success		201		{object}	tenants.CreateResponse
//	@Failure		400		{object}	map[string]string
//	@Failure		403		{object}	map[string]string
//	@Failure		409		{object}	map[string]string
//	@Router			/v1/tenants [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
	req.AdminEmail = strings.ToLower(strings.TrimSpace(req.AdminEmail))
	req.AdminName = strings.TrimSpace(req.AdminName)

	if req.Name == "" {
		httpx.Error(w, http.StatusBadRequest, "name is required")
		return
	}
	if !slugRe.MatchString(req.Slug) {
		httpx.Error(w, http.StatusBadRequest, "slug must be 3-32 characters, lowercase letters, digits or hyphens, and start and end with a letter or digit")
		return
	}
	if !strings.Contains(req.AdminEmail, "@") {
		httpx.Error(w, http.StatusBadRequest, "a valid admin_email is required")
		return
	}
	if len(req.AdminPassword) < minPasswordLen {
		httpx.Error(w, http.StatusBadRequest, "admin_password must be at least 6 characters")
		return
	}

	// Cost 12 to match internal/auth/service.go — a cheaper hash here would be
	// silently unverifiable-looking rather than broken, so it is worth pinning.
	hash, err := bcrypt.GenerateFromPassword([]byte(req.AdminPassword), 12)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "hash failed")
		return
	}

	var out CreateResponse
	// Scoped to the CALLER's tenant only until the new tenant exists; the
	// transaction re-scopes itself below. One transaction, so a failure part-way
	// cannot leave a tenant with no way to log into it.
	err = db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		var t DTO
		err := tx.QueryRow(r.Context(), `
INSERT INTO tenants (name, slug) VALUES ($1, $2)
RETURNING id, name, slug, to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF:00')`,
			req.Name, req.Slug).Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt)
		if err != nil {
			return err
		}

		// Re-scope the transaction to the tenant just created, so the rows below
		// are written under the same RLS check as every other write in the app.
		// set_config(..., true) is transaction-local, so this cannot leak to the
		// next user of this pooled connection.
		// Formatted as a string, not passed as int64: set_config's second argument
		// is text, and pgx will not encode an integer into a text parameter
		// ("cannot find encode plan"). db.Tx formats it the same way.
		if _, err := tx.Exec(r.Context(),
			`SELECT set_config('app.current_tenant', $1, true)`,
			strconv.FormatInt(t.ID, 10)); err != nil {
			return err
		}

		if err := tx.QueryRow(r.Context(), `
INSERT INTO users (tenant_id, email, password_hash, full_name, role, status)
VALUES ($1, $2, $3, NULLIF($4, ''), 'admin', 'active')
RETURNING id`, t.ID, req.AdminEmail, string(hash), req.AdminName).Scan(&out.AdminID); err != nil {
			return err
		}

		// A fleet group is not cosmetic: several screens group vehicles by it,
		// and an import into a tenant with none leaves them ungrouped.
		if _, err := tx.Exec(r.Context(), `
INSERT INTO fleet_groups (tenant_id, name, description)
VALUES ($1, 'Default Fleet', 'Created with the tenant')
ON CONFLICT (tenant_id, name) DO NOTHING`, t.ID); err != nil {
			return err
		}

		t.Users, t.Vehicles = 1, 0
		out.Tenant, out.AdminEmail = t, req.AdminEmail
		return nil
	})
	if err != nil {
		// Both uniqueness failures are ordinary operator mistakes, so they get a
		// message naming the field rather than a generic 500. Emails are unique
		// GLOBALLY, not per tenant, which is surprising enough to spell out.
		if code := pgUniqueViolation(err); code != "" {
			switch {
			case strings.Contains(code, "slug"):
				httpx.Error(w, http.StatusConflict, "that slug is already taken")
			case strings.Contains(code, "email"):
				httpx.Error(w, http.StatusConflict, "that email already belongs to another account (emails are unique across all tenants)")
			default:
				httpx.Error(w, http.StatusConflict, "tenant or admin already exists")
			}
			return
		}
		// Logged server-side: the client gets a safe message, but a provisioning
		// failure with no trace is nearly impossible to diagnose afterwards.
		log.Printf("tenants: create %q/%q failed: %v", req.Slug, req.AdminEmail, err)
		httpx.Error(w, http.StatusBadRequest, "create tenant failed")
		return
	}
	httpx.JSON(w, http.StatusCreated, out)
}

// pgUniqueViolation returns the violated constraint's name for a 23505, else "".
//
// The constraint name is what distinguishes "slug taken" from "email taken";
// matching on the error message text would break the first time Postgres
// rewords it.
func pgUniqueViolation(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return pgErr.ConstraintName
	}
	return ""
}
