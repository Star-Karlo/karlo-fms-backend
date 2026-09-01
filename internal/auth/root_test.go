package auth

import (
	"context"
	"testing"
)

// IsRoot must be true ONLY for a platform_admin whose token carries root. A child
// platform_admin, a mislabelled non-platform user, and an anonymous caller must
// all read as not-root — that boolean is what gates managing other platform admins.
func TestIsRoot(t *testing.T) {
	cases := []struct {
		name  string
		claim *Claims
		want  bool
	}{
		{"root platform admin", &Claims{Role: RolePlatformAdmin, IsRoot: true}, true},
		{"child platform admin", &Claims{Role: RolePlatformAdmin, IsRoot: false}, false},
		{"admin cannot be root", &Claims{Role: RoleAdmin, IsRoot: true}, false}, // root only means anything for platform_admin
		{"operator", &Claims{Role: RoleOperator}, false},
		{"anonymous", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if c.claim != nil {
				ctx = WithClaims(ctx, c.claim)
			}
			if got := IsRoot(ctx); got != c.want {
				t.Fatalf("IsRoot = %v, want %v", got, c.want)
			}
		})
	}
}
