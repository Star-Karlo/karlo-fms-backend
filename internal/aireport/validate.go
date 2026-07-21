package aireport

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/karlo/fms-backend/internal/customreport"
	"github.com/karlo/fms-backend/internal/report"
)

// validate turns the model's tool input into a spec that will actually render,
// and returns problems phrased for the repair pass.
//
// The rule throughout: DROP what cannot work, KEEP what can, and always say
// what was dropped. A section referencing a report that does not exist is not
// worth failing the whole report over, but it must not vanish silently either —
// the user would be looking at a report missing something they asked for, with
// nothing on screen to say so.
//
// Messages name both the offending value and the legal set, because this text
// is what the model reads to fix itself. "invalid source" tells it nothing;
// "unknown source X; valid: a, b, c" tells it exactly what to change.
func validate(input map[string]any) (customreport.Spec, []string, []string) {
	var problems []string

	spec := customreport.Spec{
		Version: customreport.SpecVersion,
		Name:    strings.TrimSpace(str(input["name"])),
		Notes:   strings.TrimSpace(str(input["notes"])),
	}
	if spec.Name == "" {
		spec.Name = "Custom Report"
		problems = append(problems, `"name" was missing; give the report a short, specific name`)
	}
	if len(spec.Name) > 120 {
		spec.Name = spec.Name[:120]
	}

	spec.Range = parseRange(input["range"], &problems)

	sources := customreport.KnownSources()
	seenSections := map[string]bool{}
	raw, _ := input["sections"].([]any)
	if len(raw) == 0 {
		problems = append(problems, "no sections; a report needs at least one")
	}

	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			problems = append(problems, fmt.Sprintf("section %d is not an object", i+1))
			continue
		}
		src := strings.TrimSpace(str(m["source"]))
		if !sources[src] {
			problems = append(problems, fmt.Sprintf(
				"section %d: unknown source %q (dropped); valid sources: %s", i+1, src, keyList(sources)))
			continue
		}

		sec := customreport.Section{
			Title:  strings.TrimSpace(str(m["title"])),
			Source: src,
			Note:   strings.TrimSpace(str(m["note"])),
			Params: cleanParams(m["params"], i, &problems),
		}

		// A per-vehicle report with no vehicle is a guaranteed runtime failure,
		// and it is worth a repair round rather than a section that renders as
		// "Could not be generated". The model is told which sources need one.
		if perVehicle[src] && sec.Params["vehicle_id"] == nil {
			problems = append(problems, fmt.Sprintf(
				"section %d (%s): this report covers ONE vehicle and needs params.vehicle_id; either supply it or use a fleet-wide source",
				i+1, src))
			continue
		}

		sec.Columns = cleanColumns(m["columns"], src, i, &problems)

		// The same source twice with the same params is the same table twice.
		// It happens when the model is boxed in — asked for something only a
		// per-vehicle report covers, it falls back to a fleet-wide one and
		// reaches for it again under a different title. Two identical tables
		// with different headings is worse than one, because the reader spends
		// time working out how they differ.
		if key := src + "|" + paramKey(sec.Params); seenSections[key] {
			problems = append(problems, fmt.Sprintf(
				"section %d duplicates an earlier %s section with the same parameters (dropped); use a different source or different params",
				i+1, src))
			continue
		} else {
			seenSections[key] = true
		}

		spec.Sections = append(spec.Sections, sec)
	}

	if len(spec.Sections) > 8 {
		problems = append(problems, fmt.Sprintf("%d sections is too many; keep 8 or fewer", len(spec.Sections)))
		spec.Sections = spec.Sections[:8]
	}

	unmapped := stringList(input["unmapped"])
	return spec, unmapped, problems
}

// parseRange reads the window, defaulting rather than failing.
//
// A missing range is recoverable — a week is the commonest report period and a
// visible default the user can change beats an error — so it is reported as a
// problem but still produces something runnable.
func parseRange(v any, problems *[]string) customreport.Range {
	m, ok := v.(map[string]any)
	if !ok {
		*problems = append(*problems, `"range" was missing; using last_7_days`)
		return customreport.Range{Preset: customreport.Range7Days}
	}
	preset := strings.TrimSpace(str(m["preset"]))
	if preset != "" {
		r := customreport.Range{Preset: customreport.RangePreset(preset)}
		if err := (&customreport.Spec{
			Name: "x", Range: r, Sections: []customreport.Section{{Source: firstSource()}},
		}).Validate(customreport.KnownSources()); err != nil && strings.Contains(err.Error(), "range") {
			*problems = append(*problems, fmt.Sprintf(
				"unknown range preset %q; valid: today, yesterday, last_7_days, last_30_days, this_month, last_month", preset))
			return customreport.Range{Preset: customreport.Range7Days}
		}
		return r
	}
	from, to := strings.TrimSpace(str(m["from"])), strings.TrimSpace(str(m["to"]))
	if from == "" || to == "" {
		*problems = append(*problems, `"range" needs either a preset or both from and to; using last_7_days`)
		return customreport.Range{Preset: customreport.Range7Days}
	}
	return customreport.Range{From: from, To: to}
}

// firstSource returns any valid source, for the throwaway spec parseRange uses
// to reach the range validator without duplicating its rules here.
func firstSource() string {
	for _, m := range report.Catalog {
		if m.Runnable {
			return m.Key
		}
	}
	return ""
}

// cleanParams keeps only the parameters a report actually accepts.
//
// The model invents plausible ones ("driver_id", "fleet_group"), and an unknown
// param is silently ignored by the runner — so the report would come back
// unfiltered and look correct. Dropping it loudly is the difference between a
// wrong report and a report with a note on it.
func cleanParams(v any, i int, problems *[]string) map[string]any {
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 {
		return nil
	}
	out := map[string]any{}
	for k, val := range m {
		switch strings.ToLower(k) {
		case "vehicle_id":
			// A vehicle id must be a real id. Asked for a fleet-wide report
			// from a per-vehicle source, the model reaches for vehicle_id:
			// "all" — which satisfies a nil check, fails to parse downstream,
			// and renders as "Could not be generated". Rejecting it here turns
			// a broken section into a repair round that produces a fleet-wide
			// source instead.
			id, ok := positiveInt(val)
			if !ok {
				*problems = append(*problems, fmt.Sprintf(
					"section %d: vehicle_id must be a single numeric vehicle id, not %v. There is no \"all\" — for the whole fleet, use a source that does not need a vehicle",
					i+1, val))
				continue
			}
			out["vehicle_id"] = id
		case "alert_code", "limit":
			out[strings.ToLower(k)] = val
		default:
			*problems = append(*problems, fmt.Sprintf(
				"section %d: param %q is not supported (dropped); only vehicle_id, alert_code and limit are", i+1, k))
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanColumns keeps only columns the source actually produces.
//
// This is where a spreadsheet-matching request goes wrong most often: asked to
// produce a column called "Fuel Used", the model reaches for a key that sounds
// right rather than one that exists. A column the source cannot supply renders
// as nothing, so it is dropped here and named — which is also how the repair
// pass learns to put it in "unmapped" instead.
func cleanColumns(v any, source string, i int, problems *[]string) []customreport.Column {
	raw, ok := v.([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	valid := map[string]bool{}
	var available []string
	for _, c := range report.ColumnsFor(source) {
		valid[c.Key] = true
		available = append(available, c.Key)
	}

	var out []customreport.Column
	var bad []string
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		key := strings.TrimSpace(str(m["key"]))
		if !valid[key] {
			bad = append(bad, key)
			continue
		}
		out = append(out, customreport.Column{Key: key, Label: strings.TrimSpace(str(m["label"]))})
	}
	if len(bad) > 0 {
		*problems = append(*problems, fmt.Sprintf(
			"section %d (%s): column(s) %s do not exist in this report (dropped); available: %s. If the user asked for one of these, list it in \"unmapped\" instead of inventing a key",
			i+1, source, strings.Join(bad, ", "), strings.Join(available, ", ")))
	}
	// Every column was invalid — fall back to all of them rather than emitting
	// a section with no columns, which renders as an empty table.
	if len(out) == 0 {
		return nil
	}
	return out
}

// ---- small helpers ----------------------------------------------------------

// paramKey is a stable identity for a param set, for duplicate detection.
func paramKey(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s=%v;", k, p[k])
	}
	return b.String()
}

// positiveInt accepts the shapes JSON gives an id: a number, or a string that
// is entirely digits.
func positiveInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n > 0 && n == float64(int64(n)) {
			return int64(n), true
		}
	case string:
		if id, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64); err == nil && id > 0 {
			return id, true
		}
	}
	return 0, false
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func stringList(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, item := range raw {
		if s := strings.TrimSpace(str(item)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func keyList(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
