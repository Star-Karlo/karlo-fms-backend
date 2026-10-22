package telemetrysync

import (
	"testing"

	"github.com/karlo/fms-backend/internal/site"
)

func TestRuleCovers(t *testing.T) {
	all := gfRule{code: "GEOFENCE_ENTER"}
	if !all.covers(7, 42) {
		t.Error("a rule naming no geofence and no vehicle must cover everything — that is what the empty form means")
	}
	oneSite := gfRule{geofences: map[int64]bool{7: true}}
	if !oneSite.covers(7, 42) || oneSite.covers(8, 42) {
		t.Error("a rule naming one geofence must cover that one and no other")
	}
	oneTruck := gfRule{vehicles: map[int64]bool{42: true}}
	if !oneTruck.covers(7, 42) || oneTruck.covers(7, 43) {
		t.Error("a rule naming one vehicle must cover that one and no other")
	}
	both := gfRule{geofences: map[int64]bool{7: true}, vehicles: map[int64]bool{42: true}}
	if both.covers(7, 43) || both.covers(8, 42) {
		t.Error("naming both narrows by both, not by either")
	}
}

// The old behaviour — attach a vehicle to a geofence and get told about it —
// has to keep working for accounts that never create a rule.
func TestTransitionAlertsHonourAttachmentAndRules(t *testing.T) {
	gf := site.Site{Kind: site.Geofence, ID: 7, Name: "Gudang Semarang"}
	trans := []site.Transition{
		{Site: gf, VehicleID: 42, Entered: true},
		{Site: gf, VehicleID: 43, Exited: true, DwellMin: 200},
		{Site: site.Site{Kind: site.Hangar, ID: 1, Name: "Bengkel"}, VehicleID: 42, Entered: true},
	}

	// No rules, no attachment: nothing is announced, though the visits were
	// still recorded by the caller.
	if got := gfRules(nil).transitionAlerts(trans, nil); len(got) != 0 {
		t.Errorf("expected no alerts without a rule or an attachment, got %d", len(got))
	}

	// Attachment alone is enough.
	attached := map[pair]bool{{42, 7}: true}
	got := gfRules(nil).transitionAlerts(trans, attached)
	if len(got) != 1 || got[0].code != "GEOFENCE_ENTER" || got[0].vid != 42 {
		t.Fatalf("attachment should produce one enter alert for vehicle 42, got %+v", got)
	}

	// A rule alone is enough, and the departure says how long the stay was.
	rules := gfRules{{code: "GEOFENCE_EXIT", urgency: "urgent"}}
	got = rules.transitionAlerts(trans, nil)
	if len(got) != 1 || got[0].code != "GEOFENCE_EXIT" {
		t.Fatalf("expected one exit alert, got %+v", got)
	}
	if got[0].severity != "urgent" {
		t.Errorf("the rule's urgency should win, got %q", got[0].severity)
	}
	if want := "Left Gudang Semarang after 3 h 20 min"; got[0].name != want {
		t.Errorf("alert name = %q, want %q", got[0].name, want)
	}

	// Hangar arrivals are shown on the page, never announced.
	for _, a := range rules.transitionAlerts(trans, attached) {
		if a.name == "Entered Bengkel" {
			t.Error("a hangar arrival should not raise an alert")
		}
	}
}

func TestHumanMinutes(t *testing.T) {
	cases := map[float64]string{0: "0 min", 45: "45 min", 60: "1 h", 90: "1 h 30 min", 200.4: "3 h 20 min"}
	for in, want := range cases {
		if got := humanMinutes(in); got != want {
			t.Errorf("humanMinutes(%v) = %q, want %q", in, got, want)
		}
	}
}
