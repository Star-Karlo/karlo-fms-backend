package customreport

import (
	"strings"
	"testing"
	"time"
)

// Presets resolve against FLEET-LOCAL days. Computed in UTC, "yesterday" would
// start at 07:00 local and cut a night shift in half.
func TestPresetsResolveOnLocalDays(t *testing.T) {
	jkt, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	// 21 July 02:00 local — deliberately inside the hours where a UTC-based
	// day boundary would still be on the 20th.
	now := time.Date(2026, 7, 21, 2, 0, 0, 0, jkt)

	from, to, err := Range{Preset: RangeYesterday}.Resolve(now, jkt)
	if err != nil {
		t.Fatal(err)
	}
	if got := from.In(jkt).Format("2006-01-02 15:04"); got != "2026-07-20 00:00" {
		t.Errorf("yesterday starts %s, want 2026-07-20 00:00 local", got)
	}
	if got := to.In(jkt).Format("2006-01-02 15:04"); got != "2026-07-21 00:00" {
		t.Errorf("yesterday ends %s, want 2026-07-21 00:00 local", got)
	}

	// "Last 7 days" asked for at 02:00 must include today so far, not stop at
	// midnight — otherwise the report silently omits the current shift.
	from, to, _ = Range{Preset: Range7Days}.Resolve(now, jkt)
	if !to.Equal(now) {
		t.Errorf("last_7_days ends %v, want now", to)
	}
	if got := from.In(jkt).Format("2006-01-02"); got != "2026-07-15" {
		t.Errorf("last_7_days starts %s, want 2026-07-15", got)
	}
}

// An absolute range comes from a browser as UTC. Labelling it as parsed made a
// report for 1–21 July announce itself as "30 Jun to 21 Jul" — rows right,
// heading a day out.
func TestAbsoluteRangeLabelsInLocalTime(t *testing.T) {
	jkt, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	r := Range{From: "2026-06-30T17:00:00Z", To: "2026-07-21T16:59:00Z"} // 1–21 July WIB
	from, to, err := r.Resolve(time.Now(), jkt)
	if err != nil {
		t.Fatal(err)
	}
	label := r.Label(from.In(jkt), to.In(jkt))
	if !strings.Contains(label, "1 Jul 2026") || !strings.Contains(label, "21 Jul 2026") {
		t.Errorf("label = %q, want the local July dates the user picked", label)
	}
	if strings.Contains(label, "30 Jun") {
		t.Errorf("label = %q, still showing the UTC day", label)
	}
}

func TestRejectsBackwardsRange(t *testing.T) {
	_, _, err := Range{From: "2026-07-21T00:00:00Z", To: "2026-07-01T00:00:00Z"}.Resolve(time.Now(), time.UTC)
	if err == nil {
		t.Error("a range ending before it starts should be rejected, not silently empty")
	}
}
