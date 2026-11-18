package dashcam

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/customreport"
)

func TestSummaryDoc(t *testing.T) {
	plateA, plateB := "B 1 A", "B 2 B"
	s := ClipSummaryDTO{
		From: "2026-11-12", To: "2026-11-18",
		Totals: ClipTotalsDTO{Events: 3, Bytes: 5_550_000,
			EventOutcomesDTO: EventOutcomesDTO{EventsWithVideo: 2, EventsCameraDidntSend: 1}},
		Types: []ClipTypeDTO{{Name: "Yawning (DMS) · 122", Events: 3, Bytes: 5_550_000,
			EventOutcomesDTO: EventOutcomesDTO{EventsWithVideo: 2, EventsCameraDidntSend: 1}}},
		Trucks: []ClipTruckDTO{
			{LicensePlate: &plateA, Events: 1, LastEvent: "2026-11-17T08:00:00+00:00"},
			{LicensePlate: &plateB, Events: 2, Bytes: 5_550_000, LastEvent: "2026-11-18T13:42:00+00:00",
				EventOutcomesDTO: EventOutcomesDTO{EventsWithVideo: 2}},
		},
	}
	doc := summaryDoc(s, time.Date(2026, 11, 18, 14, 5, 0, 0, wib))

	if doc.RangeLabel != "12 Nov 2026 – 18 Nov 2026" {
		t.Errorf("range %q", doc.RangeLabel)
	}
	var titles []string
	for _, sec := range doc.Sections {
		titles = append(titles, sec.Title)
	}
	if strings.Join(titles, "|") != "Overview|Per event type|Trucks|Definitions" {
		t.Fatalf("sections %v", titles)
	}
	overview := map[string]string{}
	for _, r := range doc.Sections[0].Rows {
		overview[r[0]] = r[1]
	}
	for k, want := range map[string]string{
		"Events": "3", "Events with video": "2 of total 3 events (67% of events have video)", "Events without video": "1",
		"Camera didn't send it": "1", "Save failed": "0", "Total MB": "5.5 MB",
	} {
		if overview[k] != want {
			t.Errorf("overview %q = %q, want %q", k, overview[k], want)
		}
	}
	if got := strings.Join(doc.Sections[1].Rows[0], "|"); got != "Yawning (DMS) · 122|3|2|1|0|5.5" {
		t.Errorf("per event type row %q, want events, with video, both causes, MB", got)
	}
	trucks := doc.Sections[2].Rows
	if trucks[0][0] != plateB || trucks[0][6] != "18 Nov 2026 13:42" || trucks[1][0] != plateA {
		t.Errorf("trucks not most events first: %v", trucks)
	}
	number := regexp.MustCompile(`(^|[^A-Za-z])[0-9]`) // a number, not the 3 in "S3"
	for _, r := range doc.Sections[3].Rows {
		if number.MatchString(r[1]) {
			t.Errorf("definition %q quotes a number; settings differ per environment", r[0])
		}
	}

	out, err := customreport.Render(doc, customreport.FormatPDF)
	if err != nil || !bytes.HasPrefix(out.Body, []byte("%PDF")) {
		t.Fatalf("render: %v", err)
	}
}

func TestPercent(t *testing.T) {
	for _, c := range []struct {
		part, whole int64
		want        string
	}{{0, 0, "—"}, {195, 195, "100%"}, {1, 3, "33%"}, {0, 5, "0%"}} {
		if got := percent(c.part, c.whole); got != c.want {
			t.Errorf("percent(%d, %d) = %q, want %q", c.part, c.whole, got, c.want)
		}
	}
}
