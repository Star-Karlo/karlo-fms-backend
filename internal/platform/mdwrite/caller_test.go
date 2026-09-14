package mdwrite

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
)

type fakeResolver map[int64]string

func (f fakeResolver) CompanyForTenant(_ context.Context, id int64) (string, bool) {
	c, ok := f[id]
	return c, ok
}

// The bug this guards: FMS's own UI scopes a staff action with ?account_id=N,
// never with X-Acting-For. A write that forwarded only the header reached
// master data with no acting-for, and master data filed the record under the
// staff member's OWN company. The vehicle appeared in FMS under the customer
// and in TMS under Karlo Platform.
func TestCallerFor(t *testing.T) {
	res := fakeResolver{7: "company-raja-besi"}
	t.Run("staff acting via account_id gets the customer's company", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/vehicles?account_id=7", nil)
		r.Header.Set("Authorization", "Bearer staff")
		c, err := CallerFor(r, 1 /* own: Karlo */, 7 /* effective: Raja Besi */, res)
		if err != nil {
			t.Fatal(err)
		}
		if c.ActingFor != "company-raja-besi" {
			t.Fatalf("ActingFor = %q, want the customer's company", c.ActingFor)
		}
	})

	t.Run("a customer acting on their own tenant sends no acting-for", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/vehicles", nil)
		c, err := CallerFor(r, 7, 7, res)
		if err != nil {
			t.Fatal(err)
		}
		if c.ActingFor != "" {
			// Master data refuses X-Acting-For from a non-staff token, so
			// adding one for a customer would break their own writes.
			t.Fatalf("ActingFor = %q, want empty", c.ActingFor)
		}
	})

	t.Run("an explicit header is forwarded verbatim", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/vehicles?account_id=7", nil)
		r.Header.Set("X-Acting-For", "company-explicit")
		c, err := CallerFor(r, 1, 7, res)
		if err != nil {
			t.Fatal(err)
		}
		if c.ActingFor != "company-explicit" {
			t.Fatalf("ActingFor = %q, want the header", c.ActingFor)
		}
	})

	t.Run("an unresolvable tenant is refused, not misfiled", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/vehicles?account_id=999", nil)
		_, err := CallerFor(r, 1, 999, res)
		if !errors.Is(err, ErrUnresolvedActingFor) {
			t.Fatalf("err = %v, want ErrUnresolvedActingFor", err)
		}
	})

	t.Run("no resolver at all is refused for cross-tenant writes", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/v1/vehicles?account_id=7", nil)
		_, err := CallerFor(r, 1, 7, nil)
		if !errors.Is(err, ErrUnresolvedActingFor) {
			t.Fatalf("err = %v, want ErrUnresolvedActingFor", err)
		}
	})
}
