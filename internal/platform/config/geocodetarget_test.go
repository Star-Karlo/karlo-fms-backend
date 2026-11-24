package config

import "testing"

// Place search and the router shared ROUTE_CACHE_URL until 2026-11-24, which
// is why services.tf said that variable was "deliberately left alone":
// repointing routing at the Maps gateway would have taken the map pickers'
// place search with it. These cases pin the split AND the guarantee that a
// deployment setting neither new variable behaves exactly as before.
func TestGeocodeTarget(t *testing.T) {
	for _, c := range []struct {
		name                string
		cfg                 Config
		wantBase, wantToken string
	}{
		{
			name:     "neither set falls back to the route cache and service token",
			cfg:      Config{RouteCacheURL: "http://fms-geocode.karlo.internal:5007", ServiceToken: "svc"},
			wantBase: "http://fms-geocode.karlo.internal:5007", wantToken: "svc",
		},
		{
			name: "base set without a token keeps the service token",
			cfg: Config{
				GeocodeBaseURL: "http://maps-gateway.karlo.internal:5006/v1/geocode",
				RouteCacheURL:  "http://fms-geocode.karlo.internal:5007", ServiceToken: "svc",
			},
			wantBase: "http://maps-gateway.karlo.internal:5006/v1/geocode", wantToken: "svc",
		},
		{
			name: "both set are used, and the route cache is not consulted",
			cfg: Config{
				GeocodeBaseURL: "http://maps-gateway.karlo.internal:5006/v1/geocode",
				GeocodeToken:   "gateway-key",
				RouteCacheURL:  "http://fms-geocode.karlo.internal:5007", ServiceToken: "svc",
			},
			wantBase: "http://maps-gateway.karlo.internal:5006/v1/geocode", wantToken: "gateway-key",
		},
		{
			name:     "nothing configured at all stays empty rather than inventing a host",
			cfg:      Config{},
			wantBase: "", wantToken: "",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, token := c.cfg.GeocodeTarget()
			if base != c.wantBase || token != c.wantToken {
				t.Errorf("got %q/%q, want %q/%q", base, token, c.wantBase, c.wantToken)
			}
		})
	}
}

// A token must never travel to a base the operator did not set: if the
// gateway URL is absent, the gateway key must not be sent to the old service.
func TestGeocodeTokenDoesNotLeakToTheFallbackBase(t *testing.T) {
	cfg := Config{
		GeocodeToken:  "gateway-key",
		RouteCacheURL: "http://fms-geocode.karlo.internal:5007",
		ServiceToken:  "svc",
	}
	base, token := cfg.GeocodeTarget()
	if base != cfg.RouteCacheURL {
		t.Fatalf("base = %q", base)
	}
	if token == "gateway-key" {
		t.Error("the gateway key was sent to the old geocode service")
	}
}
