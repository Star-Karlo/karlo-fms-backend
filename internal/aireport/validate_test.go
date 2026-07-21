package aireport

import (
	"strings"
	"testing"
)

// Every case here is something Nova Pro actually produced against the live
// catalog, not something invented to exercise a branch. Bedrock cannot enforce
// the tool schema, so this validator is the only thing between the model and a
// report that renders wrong.

func TestRejectsInventedVehicleID(t *testing.T) {
	// Observed: asked for a fleet-wide report from a per-vehicle source, the
	// model emitted vehicle_id:"all". It satisfies a nil check, fails to parse
	// downstream, and renders as "Could not be generated" — so the user gets a
	// broken section instead of a fleet-wide source.
	for _, bad := range []any{"all", "ALL", "", float64(0), float64(-3), "abc", 1.5, nil} {
		spec, _, problems := validate(map[string]any{
			"name":  "R",
			"range": map[string]any{"preset": "last_7_days"},
			"sections": []any{map[string]any{
				"source": "speeding-violations",
				"params": map[string]any{"vehicle_id": bad},
			}},
		})
		if len(spec.Sections) != 0 {
			t.Errorf("vehicle_id=%v was accepted; section survived", bad)
		}
		if len(problems) == 0 {
			t.Errorf("vehicle_id=%v produced no problem for the repair pass", bad)
		}
	}
}

func TestAcceptsRealVehicleID(t *testing.T) {
	for _, good := range []any{float64(2569), "2569"} {
		spec, _, problems := validate(map[string]any{
			"name":  "R",
			"range": map[string]any{"preset": "last_30_days"},
			"sections": []any{map[string]any{
				"source": "speeding-violations",
				"params": map[string]any{"vehicle_id": good},
			}},
		})
		if len(spec.Sections) != 1 {
			t.Fatalf("vehicle_id=%v was rejected: %v", good, problems)
		}
		if got := spec.Sections[0].Params["vehicle_id"]; got != int64(2569) {
			t.Errorf("vehicle_id = %#v, want int64(2569)", got)
		}
	}
}

func TestDropsUnknownSourceButKeepsTheRest(t *testing.T) {
	// A hallucinated source must not cost the sections that were fine.
	spec, _, problems := validate(map[string]any{
		"name":  "R",
		"range": map[string]any{"preset": "this_month"},
		"sections": []any{
			map[string]any{"source": "fuel-efficiency-by-driver"}, // does not exist
			map[string]any{"source": "vehicle-alerts-summary"},
		},
	})
	if len(spec.Sections) != 1 || spec.Sections[0].Source != "vehicle-alerts-summary" {
		t.Fatalf("sections = %+v, want only the valid one", spec.Sections)
	}
	joined := strings.Join(problems, " ")
	if !strings.Contains(joined, "fuel-efficiency-by-driver") || !strings.Contains(joined, "valid sources") {
		t.Errorf("problem must name the bad source AND the legal set, got: %q", joined)
	}
}

func TestDropsColumnsTheSourceDoesNotHave(t *testing.T) {
	// Asked to match a spreadsheet, the model reaches for column keys that
	// sound right. An unknown key renders as nothing, so it is dropped — and
	// named, so the repair pass learns to use "unmapped" instead.
	spec, _, problems := validate(map[string]any{
		"name":  "R",
		"range": map[string]any{"preset": "last_7_days"},
		"sections": []any{map[string]any{
			"source": "vehicle-alerts-summary",
			"columns": []any{
				map[string]any{"key": "alert_name", "label": "Notifikasi"},
				map[string]any{"key": "fuel_cost"}, // no such column anywhere
			},
		}},
	})
	if len(spec.Sections) != 1 {
		t.Fatal("section was dropped entirely; one bad column must not cost the section")
	}
	cols := spec.Sections[0].Columns
	if len(cols) != 1 || cols[0].Key != "alert_name" || cols[0].Label != "Notifikasi" {
		t.Errorf("columns = %+v, want only the valid one with its rename kept", cols)
	}
	joined := strings.Join(problems, " ")
	if !strings.Contains(joined, "fuel_cost") || !strings.Contains(joined, "available") {
		t.Errorf("problem must name the bad column and what IS available, got: %q", joined)
	}
	if !strings.Contains(joined, "unmapped") {
		t.Error("the repair message should point at unmapped rather than inviting another guess")
	}
}

func TestDropsDuplicateSections(t *testing.T) {
	// Observed when the model is boxed in: it falls back to a fleet-wide
	// source and reaches for it twice under different titles. Two identical
	// tables with different headings is worse than one.
	spec, _, problems := validate(map[string]any{
		"name":  "R",
		"range": map[string]any{"preset": "last_7_days"},
		"sections": []any{
			map[string]any{"source": "vehicle-alerts-history", "title": "Trips"},
			map[string]any{"source": "vehicle-alerts-history", "title": "Speeding"},
		},
	})
	if len(spec.Sections) != 1 {
		t.Errorf("sections = %d, want 1 after dedupe", len(spec.Sections))
	}
	if !strings.Contains(strings.Join(problems, " "), "duplicate") {
		t.Error("duplication must be reported, not silently fixed")
	}
}

func TestSameSourceDifferentParamsSurvives(t *testing.T) {
	// Two vehicles' trip summaries in one report is a legitimate thing to want.
	spec, _, _ := validate(map[string]any{
		"name":  "R",
		"range": map[string]any{"preset": "last_7_days"},
		"sections": []any{
			map[string]any{"source": "vehicle-trip-summary", "params": map[string]any{"vehicle_id": float64(1)}},
			map[string]any{"source": "vehicle-trip-summary", "params": map[string]any{"vehicle_id": float64(2)}},
		},
	})
	if len(spec.Sections) != 2 {
		t.Errorf("sections = %d, want 2 — different vehicles are not duplicates", len(spec.Sections))
	}
}

func TestUnknownParamsAreDroppedAndReported(t *testing.T) {
	// The runner ignores params it does not know, so an invented filter would
	// come back UNFILTERED and look correct.
	spec, _, problems := validate(map[string]any{
		"name":  "R",
		"range": map[string]any{"preset": "last_7_days"},
		"sections": []any{map[string]any{
			"source": "vehicle-alerts-summary",
			"params": map[string]any{"driver_id": float64(7), "fleet_group": "Jakarta"},
		}},
	})
	if len(spec.Sections) != 1 || spec.Sections[0].Params != nil {
		t.Errorf("params = %+v, want none kept", spec.Sections[0].Params)
	}
	joined := strings.Join(problems, " ")
	for _, want := range []string{"driver_id", "fleet_group"} {
		if !strings.Contains(joined, want) {
			t.Errorf("problem should name %q, got: %q", want, joined)
		}
	}
}

func TestBadRangeFallsBackRatherThanFailing(t *testing.T) {
	for _, r := range []any{
		nil,
		map[string]any{"preset": "last_fortnight"},
		map[string]any{"from": "2026-07-01T00:00:00Z"}, // to missing
	} {
		spec, _, problems := validate(map[string]any{
			"name":     "R",
			"range":    r,
			"sections": []any{map[string]any{"source": "vehicle-alerts-summary"}},
		})
		if spec.Range.Preset == "" && spec.Range.From == "" {
			t.Errorf("range %v left the spec unrunnable", r)
		}
		if len(problems) == 0 {
			t.Errorf("range %v was silently accepted", r)
		}
	}
}

func TestEveryCatalogSourceIsOfferedToTheModel(t *testing.T) {
	// The catalog is derived, not hand-listed, so this guards the derivation:
	// a report the model is never shown may as well not exist.
	cat := buildCatalog()
	if len(cat) == 0 {
		t.Fatal("no sources offered")
	}
	for _, e := range cat {
		if len(e.Columns) == 0 {
			t.Errorf("%s: offered with no columns; the model cannot select or map them", e.Key)
		}
		if e.Title == "" || e.Description == "" {
			t.Errorf("%s: missing title or description", e.Key)
		}
	}
}
