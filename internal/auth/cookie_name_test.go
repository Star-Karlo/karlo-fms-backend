package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCookieName(t *testing.T) {
	for _, c := range []struct{ domain, base, want string }{
		{".karlo.id", refreshCookie, "karlo_rt"},
		{"karlo.id", sessionCookie, "karlo_session"},
		{".staging.karlo.id", refreshCookie, "karlo_rt_staging"},
		{".staging.karlo.id", sessionCookie, "karlo_session_staging"},
		{"", refreshCookie, "karlo_rt_staging"},
		{".staging.karlo.id", legacyCookie, "fms_refresh"},
	} {
		if got := (&Handler{cookieDomain: c.domain}).cookieName(c.base); got != c.want {
			t.Errorf("cookieName(%q, %q) = %q, want %q", c.domain, c.base, got, c.want)
		}
	}
}

func TestRefreshTokenFromNeedsCookieMode(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: "karlo_rt_staging", Value: "shared"})
	if got := (&Handler{}).refreshTokenFrom(r, ""); got != "" {
		t.Errorf("no cookie domain: got %q, want the shared cookie left alone", got)
	}
	if got := (&Handler{cookieDomain: ".staging.karlo.id"}).refreshTokenFrom(r, ""); got != "shared" {
		t.Errorf("cookie mode: got %q, want %q", got, "shared")
	}
	if got := (&Handler{}).refreshTokenFrom(r, "body"); got != "body" {
		t.Errorf("body token: got %q", got)
	}
}
