package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Delegating the login to the shared authentication service.
//
// The verifier in sso.go lets this service ACCEPT a token the shared IAM
// minted. This lets it stop minting its own: /v1/auth/login forwards the
// credentials upstream and returns what comes back, so a client that has not
// moved yet keeps calling the same endpoint and receives a token from the new
// issuer without knowing anything changed.
//
// WHY PROXY RATHER THAN POINT THE CLIENTS STRAIGHT AT THE AUTH SERVICE.
// Eventually they should — the proxy is a hop that earns nothing once every
// client knows the new address. It exists because the two moves are separable:
// this one is a backend deploy and a config flag, reversible by unsetting the
// flag, while repointing the web app, the mobile client and every saved
// integration is a coordinated release across three codebases. Doing them
// together means a single change that cannot be rolled back by halves.
//
// THERE IS NO FALLBACK TO THE LOCAL PASSWORD, deliberately.
//
// Falling back when the upstream refuses would mean a password changed in the
// shared IAM still opens this service with the old one, for as long as the
// local row survives — and it would fail open exactly when the auth service is
// unreachable, which is when an attacker would most like it to. So the upstream
// is authoritative once configured: its refusal is this service's refusal, and
// its outage is this service's outage. The local login remains only for
// deployments with no upstream configured at all.

// Upstream calls the shared authentication service.
type Upstream struct {
	baseURL string
	http    *http.Client
}

// NewUpstream returns nil when no base URL is configured, which leaves the
// local login in place — the state every deployment starts in.
func NewUpstream(baseURL string, timeout time.Duration) *Upstream {
	if baseURL == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Upstream{baseURL: baseURL, http: &http.Client{Timeout: timeout}}
}

// ErrUpstreamRejected is a refusal by the auth service: bad credentials, a
// suspended account, an expired refresh token. Distinguished from a transport
// failure because one is a 401 for the caller and the other is a 502 — telling
// somebody their password is wrong when the auth service is simply down sends
// them to reset a password that was never the problem.
var ErrUpstreamRejected = errors.New("rejected by the authentication service")

// ErrUpstreamThrottled is the auth service rate-limiting the caller. Distinct
// from both a refusal and an outage: the credentials may be perfectly good and
// the service is perfectly healthy.
var ErrUpstreamThrottled = errors.New("too many attempts")

// upstreamTokens is the subset of the auth service's response this service
// passes through. Its shape is not mirrored field by field on purpose: only
// the tokens are needed, and copying the rest would couple this service to a
// response body it does not read.
type upstreamTokens struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

// Login forwards credentials upstream and returns the tokens it issues, plus
// the display fields the auth service knows and the token does not.
func (u *Upstream) Login(ctx context.Context, email, password string) (*TokenResponse, *UpstreamUser, error) {
	return u.postWithUser(ctx, "/api/v1/auth/login", map[string]string{
		"email": email, "password": password,
	})
}

// UpstreamUser is the little the auth service knows that the access token does
// not carry: an email and a name, for display.
type UpstreamUser struct {
	Email    string `json:"email"`
	FullName string `json:"fullName"`
}

func (u *Upstream) postWithUser(ctx context.Context, path string, body any) (*TokenResponse, *UpstreamUser, error) {
	resp, raw, err := u.postRaw(ctx, path, body)
	if err != nil {
		return nil, nil, err
	}
	var envelope struct {
		Data struct {
			User UpstreamUser `json:"user"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &envelope)
	return resp, &envelope.Data.User, nil
}

// Refresh rotates a refresh token upstream.
func (u *Upstream) Refresh(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	return u.post(ctx, "/api/v1/auth/refresh", map[string]string{
		"refreshToken": refreshToken,
	})
}

func (u *Upstream) post(ctx context.Context, path string, body any) (*TokenResponse, error) {
	r, _, err := u.postRaw(ctx, path, body)
	return r, err
}

func (u *Upstream) postRaw(ctx context.Context, path string, body any) (*TokenResponse, []byte, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := u.http.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("authentication service unreachable: %w", err)
	}
	defer resp.Body.Close()

	// Capped: an upstream returning something enormous must not be able to
	// exhaust this service's memory through an unauthenticated endpoint.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("reading the authentication service's reply: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return nil, nil, ErrUpstreamRejected
	case resp.StatusCode == http.StatusTooManyRequests:
		// Passed through rather than folded into either bucket. It is not a
		// wrong password — telling somebody their credentials are bad when they
		// are simply too fast sends them to reset a working one — and it is not
		// an outage, so a 502 would be wrong too. The client should back off,
		// which only a 429 tells it to do.
		return nil, nil, ErrUpstreamThrottled
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return nil, nil, fmt.Errorf("authentication service returned %d", resp.StatusCode)
	}

	// Defence in depth, not a workaround.
	//
	// The auth service returns a correct 401 for a wrong password, so this
	// never fires against it today. It exists because the failure it guards
	// against is silent and severe: a 2xx whose body says success:false would
	// otherwise fall through to the token search, and a future envelope that
	// carried a token alongside a failure flag would be read as a login. An
	// authentication path should not depend on the status and the body always
	// agreeing.
	//
	// (An earlier version of this comment claimed the service DID answer 200
	// for a bad password. It does not — that was measured wrong, by printing
	// the body without the status. Recorded because the wrong reason for a
	// right check is how the check gets deleted later by somebody who tests
	// the claim and finds it false.)
	var env struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Success != nil && !*env.Success {
		return nil, nil, ErrUpstreamRejected
	}

	// The tokens may be at the top level or nested under a data/envelope key,
	// depending on which handler answered. Rather than encode that here — where
	// a change upstream would surface as an empty token and a confusing 500 —
	// find them wherever they are and fail loudly if they are not there.
	tok := findTokens(raw)
	if tok == nil || tok.AccessToken == "" {
		return nil, nil, fmt.Errorf("no access token in the authentication service's reply")
	}

	return &TokenResponse{
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresIn:    tok.ExpiresIn,
		// User is filled in by the caller, which has the verifier and can
		// therefore read the role out of the token it just received. Leaving it
		// zero — as this did — gives the browser a session with no role, and
		// every role-gated control in the app then behaves the same for
		// everyone: no edit buttons, no admin screens, no error either.
	}, raw, nil
}

// findTokens collects the token fields from anywhere in the reply.
//
// It COLLECTS rather than returning the first object that has an access token,
// and that distinction is the whole bug it was written to fix. The auth service
// answers with both a top-level `token` and a nested `tokens` object holding
// accessToken/refreshToken/expiresAt. The old version walked the tree and
// returned as soon as it saw an access token — which, because Go iterates maps
// in random order, was usually the top-level `token`, before it had ever seen
// the refresh token beside it.
//
// The symptom was not an error. Login worked, the browser got an access token,
// and the refresh token was silently empty — so every page reload logged the
// user out and it looked like a session bug rather than a parsing one.
func findTokens(raw []byte) *upstreamTokens {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	out := &upstreamTokens{}

	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for k, val := range t {
				s, _ := val.(string)
				switch k {
				case "accessToken", "access_token", "token":
					// A nested accessToken is more specific than a top-level
					// `token`, but either will do — first non-empty wins and
					// they carry the same value.
					if out.AccessToken == "" {
						out.AccessToken = s
					}
				case "refreshToken", "refresh_token":
					if out.RefreshToken == "" {
						out.RefreshToken = s
					}
				case "expiresIn", "expires_in":
					if f, ok := val.(float64); ok && out.ExpiresIn == 0 {
						out.ExpiresIn = int(f)
					}
				case "expiresAt", "expires_at":
					// An absolute RFC3339 instant, not a duration. Converted
					// here because the browser's refresh timer counts seconds.
					if ts, err := time.Parse(time.RFC3339, s); err == nil && out.ExpiresIn == 0 {
						if d := int(time.Until(ts).Seconds()); d > 0 {
							out.ExpiresIn = d
						}
					}
				}
				walk(val)
			}
		case []any:
			for _, e := range t {
				walk(e)
			}
		}
	}
	walk(doc)

	if out.AccessToken == "" {
		return nil
	}
	return out
}

// Forward proxies an authenticated request to the auth service, passing the
// caller's own bearer token through unchanged.
//
// The token is the caller's, not a service credential, so the auth service
// applies exactly the authorisation it would if the browser had called it
// directly. FMS adds nothing and can therefore widen nothing — which is the
// property that makes proxying these endpoints safe at all.
func (u *Upstream) Forward(ctx context.Context, method, path, bearer string, body any) (int, []byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.baseURL+path, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := u.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("authentication service unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, raw, nil
}

// Me reads the caller's profile from the auth service and renders it in this
// service's shape.
//
// The claims are passed in for the fields the upstream cannot know: the FMS
// tenant and user ids, and the rank this service's own RequireRole checks
// against. Those are derived from the token rather than looked up, so /me
// agrees with what every other endpoint in this service just enforced.
func (u *Upstream) Me(ctx context.Context, bearer string, c *Claims) (*UserDTO, error) {
	code, raw, err := u.Forward(ctx, http.MethodGet, "/api/v1/auth/me", bearer, nil)
	if err != nil {
		return nil, err
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return nil, ErrUpstreamRejected
	}
	if code < 200 || code >= 300 {
		return nil, fmt.Errorf("authentication service returned %d", code)
	}

	var body struct {
		Success *bool `json:"success"`
		Data    struct {
			Email    string `json:"email"`
			FullName string `json:"fullName"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("unreadable profile from the authentication service: %w", err)
	}
	if body.Success != nil && !*body.Success {
		return nil, ErrUpstreamRejected
	}

	id, _ := strconv.ParseInt(c.Subject, 10, 64)
	var name *string
	if body.Data.FullName != "" {
		n := body.Data.FullName
		name = &n
	}
	return &UserDTO{
		ID:       id,
		Email:    body.Data.Email,
		FullName: name,
		Role:     c.Role,
		// Suspension is enforced upstream — a suspended account cannot obtain a
		// token at all, so anyone reaching this line is active by construction.
		Status:   "active",
		TenantID: c.TenantID,
		IsRoot:   c.IsRoot,
	}, nil
}
