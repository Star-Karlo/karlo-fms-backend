package auth

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/karlo/fms-backend/internal/platform/authctx"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Single sign-on: accepting access tokens minted by authentication-api.karlo.id.
//
// THE DESIGN, because the obvious alternative is much worse.
//
// There are ~160 call sites reading auth.TenantID(ctx), auth.EffectiveTenant(r),
// auth.FromContext(ctx) and auth.RequireRole(...). Rewriting them to speak
// authctx.Principal would be a 160-file change landing in one commit, touching
// every handler in the service, with the tenant-isolation boundary in the
// middle of it. Nobody can review that, and RLS only protects the rows — a
// handler that reads the WRONG tenant id still gets a coherent, wrong answer.
//
// So the translation happens once, here. A verified Principal becomes the same
// *Claims the local login has always produced, and every call site downstream
// is unchanged and untouched. What moves is where Claims comes from, not what
// anything does with it.
//
// The two token formats run side by side deliberately. FMS keeps minting its
// own HS256 tokens until the frontend has moved, so a deploy of this code
// changes nothing on its own: an RS256 token from the auth service is accepted
// IF one arrives, and everything else carries on. That is what makes the
// cutover reversible — and reversible one client at a time rather than all at
// once.

// SSO verifies tokens from the shared IAM and adapts them to this service.
type SSO struct{ verifier *authctx.Verifier }

// NewSSO returns nil, nil when no public key is configured, which is the
// supported "not cut over yet" state rather than an error — a deployment that
// has not been pointed at the auth service must keep working.
func NewSSO(pemBytes []byte) (*SSO, error) {
	if len(pemBytes) == 0 {
		return nil, nil
	}
	v, err := authctx.NewVerifier(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("auth service public key: %w", err)
	}
	return &SSO{verifier: v}, nil
}

// ErrNotFMS is returned for a valid token belonging to somebody with no access
// to this product — a TMS-only account. It is a 403, not a 401: the credential
// is genuine, it just does not reach here, and telling the holder to log in
// again would send them round a loop that cannot succeed.
var ErrNotFMS = errors.New("this account has no FMS access")

// Claims translates a verified Principal into this service's claims.
func (s *SSO) Claims(token string) (*Claims, error) {
	p, err := s.verifier.Verify(token)
	if err != nil {
		return nil, err
	}

	// Platform staff administer across tenants and legitimately have no FMS
	// tenant of their own; everyone else needs the alias, because it is what
	// app.current_tenant is set to and every RLS policy compares against.
	if p.FMSTenantID == 0 && !p.IsPlatformStaff {
		return nil, ErrNotFMS
	}
	access, hasFMS := p.Access[authctx.ProductFMS]
	if !hasFMS && !p.IsPlatformStaff {
		return nil, ErrNotFMS
	}

	return &Claims{
		TenantID: p.FMSTenantID,
		Role:     roleFor(p, access),
		// Email is not in the token. Nothing in this service authorises on it —
		// it is display and audit only — so it stays empty rather than being
		// fetched per request to fill a field nobody checks.
		Email: "",
		// The two-tier platform admin is gone: the shared IAM has one staff bit
		// by design, and only bootstrap creates staff. Root therefore tracks
		// staff exactly, which keeps IsRoot() answering the same question it
		// always did rather than silently always-false.
		IsRoot: p.IsPlatformStaff,
		// Both bigint aliases travel in the token, so nothing here translates an
		// identifier at runtime: FMSTenantID is what app.current_tenant is set
		// to, and FMSUserID is what this service's own rows are keyed by
		// (dashboard_layouts.user_id, api_tokens.created_by, report owners).
		// Zero for somebody with no row here — a Karlo employee, typically, who
		// administers across tenants and owns no such rows.
		RegisteredClaims: jwt.RegisteredClaims{Subject: strconv.FormatInt(p.FMSUserID, 10)},

		// The shared IAM's own resolution — role permissions unioned with any
		// per-person extras, intersected with the company's entitlement.
		//
		// Carried so the permission middleware and the feature list answer from
		// the token rather than from fms_app.roles and fms_app.tenant_features,
		// neither of which decides anything any more. Perms is a non-nil empty
		// slice when there are none, so "resolved to nothing" stays
		// distinguishable from "this is a locally minted token".
		Perms:     append([]string{}, access.Permissions...),
		GrantsAll: access.GrantsAll || p.IsPlatformStaff,
		Features:  append([]string{}, access.Features...),
	}, nil
}

// roleFor maps shared-IAM access onto this service's rank hierarchy.
//
// The ranks survive the migration because RequireRole guards 18 routes and the
// hierarchy is what they mean. What changes is where the rank comes from: a
// role that grants everything its company holds IS this service's `admin`, and
// a Karlo employee is platform_admin regardless of any company's roles.
func roleFor(p authctx.Principal, access authctx.ProductAccess) string {
	switch {
	case p.IsPlatformStaff:
		return RolePlatformAdmin
	case access.GrantsAll:
		return RoleAdmin
	}
	// The shared IAM keeps each product's own vocabulary, so an imported role
	// still says operator/manager/admin. Anything unrecognised is the safe
	// floor rather than a guess — a role nobody mapped must not silently
	// out-rank one that was.
	if _, known := roleRank[access.Role]; known {
		return access.Role
	}
	return RoleOperator
}

// AuthenticatorWith verifies a Bearer token by either route and injects the
// resulting claims.
//
// An RS256 token from the shared IAM is tried first when SSO is configured, and
// a locally minted HS256 token still works. Both are checked because during the
// cutover both are in circulation — the web app may have moved while a mobile
// client or a saved integration has not.
func AuthenticatorWith(tm *TokenManager, sso *SSO) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := r.Header.Get("Authorization")
			if !strings.HasPrefix(h, "Bearer ") {
				httpx.Error(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			raw := strings.TrimPrefix(h, "Bearer ")

			if sso != nil {
				switch claims, err := sso.Claims(raw); {
				case err == nil:
					next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
					return
				case errors.Is(err, ErrNotFMS):
					// The token verified. Falling through to the local parser
					// would report "invalid or expired token", which sends the
					// holder to log in again and get the same answer.
					httpx.Error(w, http.StatusForbidden, ErrNotFMS.Error())
					return
				}
				// Anything else means this was not a token from the shared IAM
				// — wrong signing method, wrong key — so try the local one.
			}

			claims, err := tm.ParseAccess(raw)
			if err != nil {
				httpx.Error(w, http.StatusUnauthorized, "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithClaims(r.Context(), claims)))
		})
	}
}

// UserFromToken renders the signed-in person in this service's shape, from a
// token this service has just verified.
//
// Used to fill the login response. The role is the one FMS's own middleware
// will enforce for the rest of the session, so what the browser shows and what
// the API allows come from the same place — rather than the browser guessing
// from a shape the auth service happens to return.
func (s *SSO) UserFromToken(token, email, fullName string) UserDTO {
	c, err := s.Claims(token)
	if err != nil {
		return UserDTO{Email: email}
	}
	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	var name *string
	if fullName != "" {
		name = &fullName
	}
	return UserDTO{
		ID:       id,
		Email:    email,
		FullName: name,
		Role:     c.Role,
		Status:   "active",
		TenantID: c.TenantID,
		IsRoot:   c.IsRoot,
	}
}
