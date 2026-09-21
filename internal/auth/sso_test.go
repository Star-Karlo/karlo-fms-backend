package auth_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/authctx"
)

// A token minted the way the authentication service mints one, so the test
// exercises the real verifier rather than a stand-in for it.
func issue(t *testing.T, priv *rsa.PrivateKey, p authctx.Principal) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, &authctx.Claims{
		Principal: p,
		RegisteredClaims: jwt.RegisteredClaims{
			// Required: the verifier rejects any other issuer, which is what
			// stops a token from some other RS256 service being accepted here.
			Issuer:    authctx.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	})
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func keypair(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return priv, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

func mustTokenManager(t *testing.T) *auth.TokenManager {
	t.Helper()
	return auth.NewTokenManager("test-secret-not-used-anywhere", time.Hour, 24*time.Hour)
}

// The whole point of the translation layer: a token from the shared IAM must
// produce exactly the claims the ~160 existing call sites already read, so that
// none of them has to change.
func TestSharedIAMTokenBecomesLocalClaims(t *testing.T) {
	priv, pub := keypair(t)
	sso, err := auth.NewSSO(pub)
	if err != nil {
		t.Fatalf("NewSSO: %v", err)
	}

	tests := []struct {
		name     string
		p        authctx.Principal
		wantTid  int64
		wantRole string
		wantUID  string
		wantRoot bool
	}{
		{
			name: "ordinary member keeps the product's own rank",
			p: authctx.Principal{
				UserID: "11111111-2222-3333-4444-555555555555", FMSTenantID: 7, FMSUserID: 42,
				Access: map[authctx.Product]authctx.ProductAccess{
					authctx.ProductFMS: {Role: "manager", Permissions: []string{"live.view"}},
				},
			},
			wantTid: 7, wantRole: auth.RoleManager, wantUID: "42",
		},
		{
			// grants_all is what an administrator role means in the shared IAM;
			// this service spells the same thing 'admin'.
			name: "a role granting everything is this service's admin",
			p: authctx.Principal{
				UserID: "11111111-2222-3333-4444-555555555555", FMSTenantID: 7, FMSUserID: 42,
				Access: map[authctx.Product]authctx.ProductAccess{
					authctx.ProductFMS: {Role: "Administrator", GrantsAll: true},
				},
			},
			wantTid: 7, wantRole: auth.RoleAdmin, wantUID: "42",
		},
		{
			// Karlo staff have no FMS tenant of their own and must still get in:
			// cross-tenant administration is the job.
			name: "platform staff need no tenant of their own",
			p: authctx.Principal{
				UserID: "11111111-2222-3333-4444-555555555555", IsPlatformStaff: true, FMSUserID: 42,
			},
			wantTid: 0, wantRole: auth.RolePlatformAdmin, wantUID: "42", wantRoot: true,
		},
		{
			// A role nobody mapped must not out-rank one that was. The floor is
			// the safe answer; guessing upward is not.
			name: "an unrecognised role falls to the floor",
			p: authctx.Principal{
				UserID: "11111111-2222-3333-4444-555555555555", FMSTenantID: 7, FMSUserID: 42,
				Access: map[authctx.Product]authctx.ProductAccess{
					authctx.ProductFMS: {Role: "Regional Supervisor"},
				},
			},
			wantTid: 7, wantRole: auth.RoleOperator, wantUID: "42",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := sso.Claims(issue(t, priv, tc.p))
			if err != nil {
				t.Fatalf("Claims: %v", err)
			}
			if c.TenantID != tc.wantTid {
				t.Errorf("TenantID = %d, want %d — this is what app.current_tenant is set to", c.TenantID, tc.wantTid)
			}
			if c.Role != tc.wantRole {
				t.Errorf("Role = %q, want %q", c.Role, tc.wantRole)
			}
			if c.Subject != tc.wantUID {
				t.Errorf("Subject = %q, want %q", c.Subject, tc.wantUID)
			}
			if c.IsRoot != tc.wantRoot {
				t.Errorf("IsRoot = %v, want %v", c.IsRoot, tc.wantRoot)
			}
		})
	}
}

// The claims must CARRY the shared IAM's resolution, not just the identity.
//
// This is the test that was missing. An edit meant to add Perms/GrantsAll/
// Features silently did not apply, and everything still compiled and passed:
// FromSharedIAM() then returned false for every request, so the permission
// middleware quietly fell back to fms_app.roles and the feature list to
// fms_app.tenant_features — the two tables the migration had just stopped
// maintaining. Nothing failed; the wrong source simply answered.
func TestClaimsCarryTheResolvedAccess(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)

	c, err := sso.Claims(issue(t, priv, authctx.Principal{
		UserID: "11111111-2222-3333-4444-555555555555", FMSTenantID: 7, FMSUserID: 42,
		Access: map[authctx.Product]authctx.ProductAccess{
			authctx.ProductFMS: {
				Role:        "operator",
				Permissions: []string{"live.view", "vehicles.view"},
				Features:    []string{"live", "vehicle"},
			},
		},
	}))
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if !c.FromSharedIAM() {
		t.Fatal("FromSharedIAM() is false — every permission check silently falls back to the local tables")
	}
	if !c.HasPerm("live.view") || c.HasPerm("vehicles.edit") {
		t.Errorf("permissions not carried: %v", c.Perms)
	}
	if len(c.Features) != 2 {
		t.Errorf("features not carried: %v — the sidebar would show every module", c.Features)
	}
}

// Staff hold everything, and the middleware must see that from the token.
func TestStaffClaimsGrantAll(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)
	c, err := sso.Claims(issue(t, priv, authctx.Principal{
		UserID: "11111111-2222-3333-4444-555555555555", IsPlatformStaff: true, FMSUserID: 1,
	}))
	if err != nil {
		t.Fatalf("Claims: %v", err)
	}
	if !c.FromSharedIAM() || !c.HasPerm("anything.at.all") {
		t.Fatal("staff must resolve to grants-all from the token")
	}
}

// A TMS-only account holds a perfectly valid token. Answering 401 would send
// them to log in again and get the same result; the credential is not the
// problem.
func TestTMSOnlyAccountIsForbiddenNotUnauthorised(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)

	token := issue(t, priv, authctx.Principal{
		UserID: "aaaa1111-2222-3333-4444-555555555555", CompanyID: "some-tms-company",
		Access: map[authctx.Product]authctx.ProductAccess{
			authctx.ProductTMS: {Role: "transporter"},
		},
	})

	h := auth.AuthenticatorWith(mustTokenManager(t), sso, nil)(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vehicles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — a valid credential with no FMS access is not an auth failure", rec.Code)
	}
}

// The cutover is reversible only if both formats work at once: the web app may
// have moved while a mobile client or a saved integration has not.
func TestLocallyMintedTokensStillWork(t *testing.T) {
	_, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)
	tm := mustTokenManager(t)

	local, err := tm.SignAccess(9, 3, auth.RoleManager, "someone@karlo.id", false)
	if err != nil {
		t.Fatalf("SignAccess: %v", err)
	}

	var got *auth.Claims
	h := auth.AuthenticatorWith(tm, sso, nil)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, _ = auth.FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vehicles", nil)
	req.Header.Set("Authorization", "Bearer "+local)
	h.ServeHTTP(rec, req)

	if got == nil {
		t.Fatalf("local token rejected while SSO is enabled (status %d) — the cutover would be one-way", rec.Code)
	}
	if got.TenantID != 3 || got.Role != auth.RoleManager {
		t.Errorf("claims = tenant %d role %q, want tenant 3 role manager", got.TenantID, got.Role)
	}
}

// With no key configured the service must behave exactly as it did before.
func TestSSODisabledWhenNoKeyConfigured(t *testing.T) {
	sso, err := auth.NewSSO(nil)
	if err != nil {
		t.Fatalf("NewSSO: %v", err)
	}
	if sso != nil {
		t.Fatal("no key configured should disable SSO, not fail — a deployment not yet cut over must keep working")
	}
}

// ---------------------------------------------------------------------------
// Acting for a customer
// ---------------------------------------------------------------------------

// fakeDir is the two-line resolver the interface exists for.
type fakeDir map[string]int64

func (f fakeDir) TenantForCompany(_ context.Context, id string) (int64, bool) {
	t, ok := f[id]
	return t, ok
}

// actingReq runs one request through the middleware and reports the status and
// the tenant the handler actually saw — which is the only thing that matters
// here, since every RLS policy compares against it.
func actingReq(t *testing.T, token, header string, dir auth.TenantResolver, sso *auth.SSO) (int, int64) {
	t.Helper()
	var seen int64 = -1
	h := auth.AuthenticatorWith(mustTokenManager(t), sso, dir)(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if c, ok := auth.FromContext(r.Context()); ok {
				seen = c.TenantID
			}
			w.WriteHeader(http.StatusOK)
		}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/vehicles", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if header != "" {
		req.Header.Set(auth.ActingForHeader, header)
	}
	h.ServeHTTP(rec, req)
	return rec.Code, seen
}

func staffToken(t *testing.T, priv *rsa.PrivateKey) string {
	t.Helper()
	return issue(t, priv, authctx.Principal{
		UserID: "5555aaaa-1111-2222-3333-444444444444", CompanyID: "karlo-platform",
		IsPlatformStaff: true,
	})
}

// Staff carry ftid 0 — they have no tenant of their own — so without the header
// every per-tenant query matches nothing. This is the whole point of the
// feature: naming a company gives them that company's tenant.
func TestStaffActingForACompanyGetsThatTenant(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)
	dir := fakeDir{"11111111-1111-1111-1111-111111111111": 1}

	code, tenant := actingReq(t, staffToken(t, priv), "", dir, sso)
	if code != http.StatusOK || tenant != 0 {
		t.Fatalf("staff with no header should pass through with ftid 0, got %d tenant=%d", code, tenant)
	}

	code, tenant = actingReq(t, staffToken(t, priv), "11111111-1111-1111-1111-111111111111", dir, sso)
	if code != http.StatusOK {
		t.Fatalf("staff acting for a known company: got %d, want 200", code)
	}
	if tenant != 1 {
		t.Errorf("handler saw tenant %d, want 1 — the acting company's tenant", tenant)
	}
}

// The isolation boundary. Dropping the header for a non-staff caller would hand
// them their OWN data in response to a request naming somebody else's company:
// an answer that looks correct and is about the wrong customer.
func TestOnlyStaffMayActForAnotherCompany(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)
	dir := fakeDir{"11111111-1111-1111-1111-111111111111": 1}

	customer := issue(t, priv, authctx.Principal{
		UserID: "bbbb1111-2222-3333-4444-555555555555", CompanyID: "some-customer",
		FMSTenantID: 7,
		Access: map[authctx.Product]authctx.ProductAccess{
			authctx.ProductFMS: {Role: "admin", GrantsAll: true},
		},
	})

	code, tenant := actingReq(t, customer, "11111111-1111-1111-1111-111111111111", dir, sso)
	if code != http.StatusForbidden {
		t.Fatalf("a customer naming another company: got %d, want 403", code)
	}
	if tenant != -1 {
		t.Errorf("the handler ran anyway and saw tenant %d — the request must not reach it", tenant)
	}
}

// An unknown company must not fall back to the caller's own tenant. Staff would
// then read their default fleet while believing they were looking at a client's.
func TestActingForAnUnknownCompanyIsRefused(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)

	code, tenant := actingReq(t, staffToken(t, priv), "99999999-9999-9999-9999-999999999999", fakeDir{}, sso)
	if code != http.StatusNotFound {
		t.Fatalf("unknown company: got %d, want 404", code)
	}
	if tenant != -1 {
		t.Errorf("the handler ran and saw tenant %d — it must not", tenant)
	}
}

// Claims come from a cache on the SSO path, so writing the acting tenant into
// the shared value would leak one request's target into the next request that
// reuses it. Two requests, different companies, in a row.
func TestActingForDoesNotLeakBetweenRequests(t *testing.T) {
	priv, pub := keypair(t)
	sso, _ := auth.NewSSO(pub)
	dir := fakeDir{"company-a": 1, "company-b": 2}
	token := staffToken(t, priv)

	if _, tenant := actingReq(t, token, "company-a", dir, sso); tenant != 1 {
		t.Fatalf("first request saw tenant %d, want 1", tenant)
	}
	if _, tenant := actingReq(t, token, "company-b", dir, sso); tenant != 2 {
		t.Fatalf("second request saw tenant %d, want 2", tenant)
	}
	if _, tenant := actingReq(t, token, "", dir, sso); tenant != 0 {
		t.Errorf("after acting for two companies, a plain request saw tenant %d, want 0", tenant)
	}
}

// fakeResolver stands in for the IAM's ValidateToken: it answers with the
// access it is given now, and counts calls so the cache can be checked.
type fakeResolver struct {
	access authctx.ProductAccess
	calls  int
	err    error
}

func (f *fakeResolver) ValidateToken(_ context.Context, _ string) (authctx.Principal, error) {
	f.calls++
	if f.err != nil {
		return authctx.Principal{}, f.err
	}
	return authctx.Principal{Access: map[authctx.Product]authctx.ProductAccess{authctx.ProductFMS: f.access}}, nil
}

// A change on Company Access must reach the claims on the next request, not
// at the next token refresh: the live access replaces the token's copy, is
// cached per user, and the token's copy is the fallback when the IAM is down.
func TestLiveAccessReplacesTheTokensCopy(t *testing.T) {
	priv, pub := keypair(t)
	token := issue(t, priv, authctx.Principal{
		UserID: "11111111-2222-3333-4444-555555555555", FMSTenantID: 7, FMSUserID: 42,
		Access: map[authctx.Product]authctx.ProductAccess{
			authctx.ProductFMS: {Role: "operator", Features: []string{"live", "maintenance"}},
		},
	})
	live := &fakeResolver{access: authctx.ProductAccess{Role: "operator", Features: []string{"live"}}}
	sso, _ := auth.NewSSO(pub)
	sso.WithLiveAccess(live, time.Minute)

	for i := 0; i < 3; i++ {
		c, err := sso.Claims(token)
		if err != nil {
			t.Fatalf("Claims: %v", err)
		}
		if len(c.Features) != 1 || c.Features[0] != "live" {
			t.Fatalf("features %v: the token's copy won over the IAM's", c.Features)
		}
	}
	if live.calls != 1 {
		t.Errorf("IAM asked %d times for one user inside the TTL; want 1", live.calls)
	}

	down, _ := auth.NewSSO(pub)
	down.WithLiveAccess(&fakeResolver{err: errors.New("unavailable")}, time.Minute)
	c, err := down.Claims(token)
	if err != nil {
		t.Fatalf("Claims with the IAM down: %v", err)
	}
	if len(c.Features) != 2 {
		t.Errorf("features %v: with the IAM down the token's copy must stand", c.Features)
	}
}
