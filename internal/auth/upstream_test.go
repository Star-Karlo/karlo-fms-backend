package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/auth"
)

// The auth service answers a WRONG PASSWORD with HTTP 200 and
// {"success":false}. Reading the status alone calls that a successful login.
//
// This is the regression worth pinning: it was masked locally by a stale
// process still serving the pre-delegation build, so the wrong password came
// back 200 with a locally minted token and looked like a fallback bug rather
// than what it was.
func TestUpstreamRejectsSuccessFalseDespiteHTTP200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"success":false,"message":"Invalid credentials"}`))
	}))
	defer srv.Close()

	_, _, err := auth.NewUpstream(srv.URL, time.Second).Login(context.Background(), "a@b.c", "wrong")
	if err == nil {
		t.Fatal("a refusal returned no error — a wrong password would be treated as a login")
	}
	if err != auth.ErrUpstreamRejected {
		t.Fatalf("got %v, want ErrUpstreamRejected — a refusal must be a 401 for the caller, not a 502", err)
	}
}

// The reply carries a top-level `token` AND a nested `tokens` object holding
// the refresh token beside it. Returning at the first access token found — as
// this did — usually picked up the top-level one and left refresh_token empty,
// because Go iterates maps in random order.
//
// The failure was silent: login worked, and then every page reload logged the
// user out. This is the shape that produced it.
func TestUpstreamCollectsTokensFromTheWholeReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{
			"token":"header.payload.sig",
			"tokens":{"accessToken":"header.payload.sig","refreshToken":"the-refresh-token","tokenType":"Bearer"},
			"user":{"email":"a@b.c","fullName":"A B"}}}`))
	}))
	defer srv.Close()

	got, up, err := auth.NewUpstream(srv.URL, time.Second).Login(context.Background(), "a@b.c", "right")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.AccessToken == "" {
		t.Error("no access token")
	}
	if got.RefreshToken != "the-refresh-token" {
		t.Errorf("refresh token = %q — an empty one logs the user out on every reload", got.RefreshToken)
	}
	if up == nil || up.Email != "a@b.c" || up.FullName != "A B" {
		t.Errorf("upstream user not carried: %+v — the browser gets a session with no role", up)
	}
}

// The token arrives as `token` inside a {"success":true,"data":{...}} envelope,
// not as `accessToken` at the top level.
func TestUpstreamFindsTheTokenInsideTheEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"token":"header.payload.sig","refreshToken":"r","expiresIn":900}}`))
	}))
	defer srv.Close()

	got, _, err := auth.NewUpstream(srv.URL, time.Second).Login(context.Background(), "a@b.c", "right")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.AccessToken != "header.payload.sig" || got.RefreshToken != "r" || got.ExpiresIn != 900 {
		t.Errorf("got %+v", got)
	}
}

// An unreachable auth service must not read as a bad password: that sends
// somebody to reset a credential that was never the problem.
func TestUnreachableUpstreamIsNotARejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close() // nothing is listening now

	_, _, err := auth.NewUpstream(srv.URL, time.Second).Login(context.Background(), "a@b.c", "x")
	if err == nil || err == auth.ErrUpstreamRejected {
		t.Fatalf("got %v, want a transport error distinct from a rejection", err)
	}
}

// No URL configured leaves the local login in place.
func TestNoUpstreamConfigured(t *testing.T) {
	if auth.NewUpstream("", time.Second) != nil {
		t.Fatal("an empty URL must disable delegation, not configure it")
	}
}
