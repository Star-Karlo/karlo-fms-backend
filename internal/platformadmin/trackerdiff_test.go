package platformadmin

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// Overlap decides whether two records describe the same stretch, and the
// open-ended case is the one that goes wrong: a device still fitted has no
// end date, and treating that as "ended at the zero time" would make every
// live assignment look disjoint from every other record.
func TestPeriodOverlap(t *testing.T) {
	jan := period{from: at("2026-01-01T00:00:00Z"), to: at("2026-02-01T00:00:00Z")}
	feb := period{from: at("2026-02-01T00:00:00Z"), to: at("2026-03-01T00:00:00Z")}
	midJan := period{from: at("2026-01-15T00:00:00Z"), to: at("2026-02-15T00:00:00Z")}
	open := period{from: at("2026-01-15T00:00:00Z")} // still fitted

	if jan.overlaps(feb) || feb.overlaps(jan) {
		t.Error("touching periods counted as overlapping — a clean handover would read as a conflict")
	}
	if !jan.overlaps(midJan) || !midJan.overlaps(jan) {
		t.Error("straddling periods not counted as overlapping — the case that silently corrupts a report")
	}
	if !jan.overlaps(open) || !open.overlaps(jan) {
		t.Error("an open-ended (still fitted) period must overlap everything after it starts")
	}
	if open.overlaps(period{from: at("2025-01-01T00:00:00Z"), to: at("2025-06-01T00:00:00Z")}) {
		t.Error("an open-ended period overlapped a window that closed before it began")
	}
}
