package auth

import "testing"

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
