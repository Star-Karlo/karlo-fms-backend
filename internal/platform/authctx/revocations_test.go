package authctx

import (
	"testing"
	"time"
)

func TestRevocationsRule(t *testing.T) {
	r := NewRevocations()
	t0 := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	r.add(revocation{Kind: "session", ID: "jti-1"})
	r.add(revocation{Kind: "user", ID: "u-1", At: t0})
	r.add(revocation{Kind: "company", ID: "c-1", At: t0})

	cases := []struct {
		name string
		p    Principal
		iat  time.Time
		want bool
	}{
		{"revoked session", Principal{TokenID: "jti-1", UserID: "x"}, t0, true},
		{"other session", Principal{TokenID: "jti-2", UserID: "x"}, t0, false},
		{"user token issued before revocation", Principal{UserID: "u-1"}, t0.Add(-time.Minute), true},
		{"user token issued after re-login", Principal{UserID: "u-1"}, t0.Add(time.Minute), false},
		{"company token before", Principal{UserID: "y", CompanyID: "c-1"}, t0.Add(-time.Hour), true},
		{"company token after", Principal{UserID: "y", CompanyID: "c-1"}, t0.Add(time.Hour), false},
		{"nil list", Principal{TokenID: "jti-1"}, t0, false},
	}
	for _, c := range cases {
		var got bool
		if c.name == "nil list" {
			got = (*Revocations)(nil).Revoked(c.p, c.iat)
		} else {
			got = r.Revoked(c.p, c.iat)
		}
		if got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
