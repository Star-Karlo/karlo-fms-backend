package retention

import (
	"strings"
	"testing"
	"time"
)

// The self-limiting DELETEs are load-bearing: each must target its own table,
// carry a LIMIT (so a far-behind sweep never issues an unbounded delete), and
// take the cutoff as $1. A careless edit that drops the LIMIT would turn a
// batch into a full-table lock, so assert the shape here.
func TestDeleteStatements(t *testing.T) {
	cases := []struct {
		name, sql, table string
	}{
		{"webhook", delWebhook, "webhook_deliveries"},
		{"refresh", delRefresh, "refresh_tokens"},
		{"link", delLink, "tracking_links"},
		{"deadclip", delDeadClip, "video_clips"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !strings.HasPrefix(c.sql, "DELETE FROM "+c.table+" ") {
				t.Errorf("%s: does not delete from %s: %q", c.name, c.table, c.sql)
			}
			if !strings.Contains(c.sql, "LIMIT") {
				t.Errorf("%s: missing LIMIT — would delete unbounded: %q", c.name, c.sql)
			}
			if !strings.Contains(c.sql, "$1") {
				t.Errorf("%s: missing $1 cutoff parameter: %q", c.name, c.sql)
			}
		})
	}
}

// The dead-clip prune must only ever remove clips that never completed — never
// 'ready' evidence, whose lifecycle belongs to the S3 bucket.
func TestDeadClipNeverTouchesReady(t *testing.T) {
	if strings.Contains(delDeadClip, "ready") {
		t.Fatalf("dead-clip prune references 'ready' — it must leave saved evidence alone: %q", delDeadClip)
	}
	if !strings.Contains(delDeadClip, "'pending'") || !strings.Contains(delDeadClip, "'failed'") {
		t.Fatalf("dead-clip prune should target pending/failed only: %q", delDeadClip)
	}
}

func TestDaysAgo(t *testing.T) {
	got := daysAgo(30)
	want := time.Now().Add(-30 * 24 * time.Hour)
	if d := got.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("daysAgo(30) = %v, want ~%v (diff %v)", got, want, d)
	}
	// Larger window must be strictly further in the past.
	if !daysAgo(548).Before(daysAgo(30)) {
		t.Error("daysAgo(548) should be earlier than daysAgo(30)")
	}
}
