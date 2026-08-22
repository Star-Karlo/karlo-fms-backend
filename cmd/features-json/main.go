// Command features-json regenerates fms-features.json at the module root — a
// verbatim JSON copy of features.Registry, published so other services (e.g. the
// shared Karlo IAM / TMS) can diff their vendored mirror against it in CI and
// fail on drift, instead of hand-syncing. Run via `go generate ./internal/features`
// (or directly); it always writes to the module root regardless of CWD.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/karlo/fms-backend/internal/features"
)

func main() {
	b, err := json.MarshalIndent(features.Registry, "", "  ")
	if err != nil {
		log.Fatalf("marshal registry: %v", err)
	}
	b = append(b, '\n')

	root, err := moduleRoot()
	if err != nil {
		log.Fatal(err)
	}
	out := filepath.Join(root, "fms-features.json")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		log.Fatalf("write %s: %v", out, err)
	}
	log.Printf("wrote %s (%d features)", out, len(features.Registry))
}

// moduleRoot walks up from the working directory to the directory holding go.mod,
// so the artifact lands in one place whether this runs from go:generate (CWD =
// internal/features) or from the repo root.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}
