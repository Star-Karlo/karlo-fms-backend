package report

import "testing"

// Columns were lifted out of the Run() switch into a map so they can be known
// without running a report. That refactor is only safe if the map is complete —
// a missing key now yields a report with NO columns, which renders as an empty
// table rather than an error.
func TestEveryRunnableReportHasColumns(t *testing.T) {
	for _, m := range Catalog {
		if !m.Runnable {
			continue
		}
		got := ColumnsFor(m.Key)
		if len(got) == 0 {
			t.Errorf("%s: no columns declared", m.Key)
			continue
		}
		seen := map[string]bool{}
		for _, c := range got {
			if c.Key == "" || c.Label == "" {
				t.Errorf("%s: column %+v has an empty key or label", m.Key, c)
			}
			if seen[c.Key] {
				t.Errorf("%s: duplicate column key %q", m.Key, c.Key)
			}
			seen[c.Key] = true
		}
	}
}

func TestColumnsForUnknownKeyIsEmpty(t *testing.T) {
	if got := ColumnsFor("no-such-report"); got != nil {
		t.Errorf("expected nil for an unknown key, got %v", got)
	}
}
