package apitoken

import (
	"context"
	"testing"
)

type stubGrant struct {
	enabled, known bool
	asked          int64
}

func (s *stubGrant) APIEnabledForTenant(_ context.Context, tenantID int64) (bool, bool) {
	s.asked = tenantID
	return s.enabled, s.known
}

// grantWithdrawn is the rule localClaims applies, exercised here directly
// rather than re-stated — a test against its own copy of the logic passes
// while the real path is wrong.
//
// The dangerous case is `known == false`. It means the directory could not
// answer — it is unreachable, or this tenant is not an FMS company upstream —
// and reading that as "forbidden" would stop every integration in the fleet
// during an outage of a service that is not even in the request path.
func refuse(g APIGrantResolver, tenantID int64) bool {
	return grantWithdrawn(context.Background(), g, tenantID)
}

func TestAnExplicitlyDisabledCompanyIsRefused(t *testing.T) {
	g := &stubGrant{enabled: false, known: true}
	if !refuse(g, 7) {
		t.Error("a company whose API access was withdrawn kept working through a locally issued key")
	}
	if g.asked != 7 {
		t.Errorf("asked about tenant %d, want 7", g.asked)
	}
}

func TestAnEnabledCompanyIsAllowed(t *testing.T) {
	if refuse(&stubGrant{enabled: true, known: true}, 7) {
		t.Error("an enabled company was refused")
	}
}

// An outage of the directory must not look like a revocation.
func TestAnUnanswerableGrantDoesNotRefuse(t *testing.T) {
	if refuse(&stubGrant{enabled: false, known: false}, 7) {
		t.Error("the directory could not answer and the key was refused anyway; " +
			"an outage outside the request path would stop every integration")
	}
}

// A deployment that has not cut over has no directory at all, and local keys
// must behave exactly as they did before.
func TestNoDirectoryLeavesLocalKeysAlone(t *testing.T) {
	if refuse(nil, 7) {
		t.Error("a deployment with no directory refused a local key")
	}
}
