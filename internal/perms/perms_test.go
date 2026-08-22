package perms_test

import (
	"strings"
	"testing"

	"github.com/karlo/fms-backend/internal/features"
	"github.com/karlo/fms-backend/internal/perms"
)

// The gating feature is DECLARED per permission, not derived from the key prefix
// (fuel.* gates on `live`, dashboard.ai on `ai_dashboard`, dashcams.manage on
// `camera`, …). FMS is the only Karlo product exercising that indirection today,
// so these mismatches are exactly what a naive "split the key on the dot"
// simplification would silently break — pin them so it fails loudly here.
func TestGatingFeatureIsNotThePrefix(t *testing.T) {
	want := map[string]string{
		"fuel.view":         "live",
		"fuel.price_calc":   "live",
		"dashboard.ai":      "ai_dashboard",
		"dashcams.manage":   "camera",
		"share_link.create": "generate_link",
		"reports.custom":    "custom_report",
		"reports.ai":        "ai_reporting",
	}
	got := map[string]string{}
	for _, p := range perms.Catalog {
		got[p.Key] = p.Feature
	}
	for key, feat := range want {
		if got[key] != feat {
			t.Errorf("%s gates on %q, want %q — the feature is DECLARED, not the key prefix", key, got[key], feat)
		}
		prefix := key[:strings.IndexByte(key, '.')]
		if feat == prefix {
			t.Errorf("%s: case no longer exercises indirection (feature == prefix == %q)", key, feat)
		}
	}
}

// Every gating feature a permission names must be a real entitlement in
// features.Registry; otherwise the permission is unreachable, or a renamed
// feature silently orphans it. Feature "" means "always available" (ungated).
func TestEveryGatingFeatureExists(t *testing.T) {
	valid := map[string]bool{}
	for _, d := range features.Registry {
		valid[d.Key] = true
	}
	for _, p := range perms.Catalog {
		if p.Feature != "" && !valid[p.Feature] {
			t.Errorf("permission %q gates on feature %q, which is not in features.Registry", p.Key, p.Feature)
		}
	}
}

// Duplicate keys would make one entry silently unreachable.
func TestPermissionKeysUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range perms.Catalog {
		if seen[p.Key] {
			t.Errorf("duplicate permission key %q", p.Key)
		}
		seen[p.Key] = true
	}
}
