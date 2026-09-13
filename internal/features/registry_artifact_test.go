package features_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/karlo/fms-backend/internal/features"
	"github.com/karlo/fms-backend/internal/perms"
)

// fms-features.json is a PUBLISHED artifact: the authentication service vendors
// a copy and gates every FMS permission against it (authctx/catalog_fms.go, and
// decodeFMSRegistry which reads `dev` as Feature.Roadmap). Regenerating it is a
// manual `go generate`, so the Registry and the file can drift — and the drift
// is silent on both sides. It shows up as a customer who cannot reach a module
// their account says is enabled, weeks later, with nothing to point at.
//
// This is the half of the drift check that lives here. The auth service runs the
// other half: every Feature named in its catalogue must exist in the vendored
// copy and must not be a roadmap stub.
func TestPublishedArtifactMatchesRegistry(t *testing.T) {
	path := filepath.Join("..", "..", "fms-features.json")
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	want, err := json.MarshalIndent(features.Registry, "", "  ")
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	want = append(want, '\n')

	if string(onDisk) != string(want) {
		t.Errorf("fms-features.json is stale — regenerate it:\n\n"+
			"    go generate ./internal/features\n\n"+
			"registry has %d features; the file has to match byte for byte because "+
			"the auth service diffs its vendored copy against it", len(features.Registry))
	}
}

// A permission may not gate on a roadmap stub.
//
// The auth service's revoke branch — which is the mode every FMS tenant lands in
// after the migration — skips features marked `dev`, on the reasoning that
// handing out a module with nothing behind it grants access to nothing,
// visibly. So a permission gated on a stub is never granted to anyone, and the
// failure is invisible: the key is in the role, the module looks enabled in the
// editor, and the holder is refused.
//
// TestEveryGatingFeatureExists (internal/perms) checks the feature exists. This
// checks it is real.
func TestNoPermissionGatesOnARoadmapStub(t *testing.T) {
	dev := map[string]bool{}
	for _, d := range features.Registry {
		dev[d.Key] = d.Dev
	}
	for _, p := range perms.Catalog {
		if p.Feature != "" && dev[p.Feature] {
			t.Errorf("permission %q gates on %q, which is marked dev — under the auth "+
				"service's revoke mode this permission can never be granted",
				p.Key, p.Feature)
		}
	}
}
