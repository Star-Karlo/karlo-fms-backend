package dashcam

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/karlo/fms-backend/internal/customreport"
)

// summaryDoc lays the Saved Evidence summary out as a report document: an
// overview, per event type, per truck, then what every term means. The terms
// match the Saved Evidence page and its legend; keep the two worded the same.
//
// No numbers from configuration appear here (how long auto-save keeps looking,
// how often it retries): they differ per environment, and a printed report
// outlives a settings change.
func summaryDoc(s ClipSummaryDTO, generated time.Time) customreport.Doc {
	t := s.Totals
	without := t.EventsCameraDidntSend + t.EventsSaveFailed
	return customreport.Doc{
		Title:      "Saved Evidence",
		RangeLabel: rangeLabel(s.From, s.To),
		Generated:  generated,
		Sections: []customreport.DocSection{
			{
				Title:   "Overview",
				Note:    "MB = storage used in S3, not SIM data quota.",
				Headers: []string{"Metric", "Value"},
				Rows: [][]string{
					{"Events", strconv.FormatInt(t.Events, 10)},
					{"Events with video", fmt.Sprintf("%d of total %d events (%s of events have video)", t.EventsWithVideo, t.Events, percent(t.EventsWithVideo, t.Events))},
					{"Events without video", strconv.FormatInt(without, 10)},
					{"Camera didn't send it", strconv.FormatInt(t.EventsCameraDidntSend, 10)},
					{"Save failed", strconv.FormatInt(t.EventsSaveFailed, 10)},
					{"Total MB", mb(t.Bytes) + " MB"},
				},
			},
			typeSection(s.Types),
			truckSection(s.Trucks),
			definitionsSection(),
		},
	}
}

func typeSection(types []ClipTypeDTO) customreport.DocSection {
	sec := customreport.DocSection{
		Title:   "Per event type",
		Headers: []string{"Event", "Events", "With video", "Camera didn't send it", "Save failed", "MB"},
		Numeric: []bool{false, true, true, true, true, true},
		Rows:    [][]string{},
	}
	for _, ty := range types {
		sec.Rows = append(sec.Rows, []string{ty.Name, strconv.FormatInt(ty.Events, 10),
			strconv.FormatInt(ty.EventsWithVideo, 10), strconv.FormatInt(ty.EventsCameraDidntSend, 10),
			strconv.FormatInt(ty.EventsSaveFailed, 10), mb(ty.Bytes)})
	}
	return sec
}

func truckSection(trucks []ClipTruckDTO) customreport.DocSection {
	sec := customreport.DocSection{
		Title:   "Trucks",
		Headers: []string{"Plate", "Events", "With video", "Camera didn't send it", "Save failed", "MB", "Last event"},
		Numeric: []bool{false, true, true, true, true, true, false},
		Rows:    [][]string{},
	}
	sorted := append([]ClipTruckDTO(nil), trucks...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Events > sorted[j].Events })
	for _, v := range sorted {
		plate := "—"
		if v.LicensePlate != nil {
			plate = *v.LicensePlate
		}
		sec.Rows = append(sec.Rows, []string{plate, strconv.FormatInt(v.Events, 10),
			strconv.FormatInt(v.EventsWithVideo, 10), strconv.FormatInt(v.EventsCameraDidntSend, 10),
			strconv.FormatInt(v.EventsSaveFailed, 10), mb(v.Bytes), cameraTime(v.LastEvent)})
	}
	return sec
}

// summaryTerms is the legend, in the PDF's words. The page has buttons, the
// PDF doesn't, so "use Pull from SD card" reads as where to find it.
var summaryTerms = [][]string{
	{"Events", "Camera events in these dates. Each event is raised by one camera: the road camera (ADAS) or the driver camera (DMS). Its video comes from that camera. If a dashcam also sends the other camera's video for the same moment, the event still counts once."},
	{"Events with video", "Events with at least one video saved. The % is events with video ÷ events."},
	{"Events without video", "Events with no saved video: the camera didn't send it, or the save failed."},
	{"Total MB", "Space the saved videos use in our cloud storage (S3). Not SIM data usage."},
	{"Camera didn't send it", "The camera raised the event but never uploaded the video to the camera server. Check the camera and SD card. Auto-save keeps checking for a late upload for a limited time. The video can also be pulled from the SD card on the Saved Evidence page while the card still has the recording (older recordings are overwritten when the card is full)."},
	{"Save failed", "The video was on the camera server, but saving it to our storage failed after several automatic attempts. Each video's card on the Saved Evidence page says why."},
	{"- Removed from the camera server", "Pull it from the SD card on the Saved Evidence page."},
	{"- Camera server down or connection dropped", "Usually temporary."},
	{"- Truck offline or weak signal", "Try again when the truck is online."},
	{"- Our system was interrupted", "Usually temporary."},
	{"- Problem on Karlo's side, or camera linked to the wrong truck", "Tell Karlo support the plate and time."},
	{"With video / MB (tables)", "The same as above, per event type or per truck. MB is storage used by that row's saved videos."},
	{"Number after an event name", "The camera's event code, for support."},
	{"Last event", "The truck's latest event in these dates (WIB)."},
	{"Counted", "Automatic saves, and SD-card pulls made from an event's card. Clips saved by hand from Recordings are not counted. Deleted videos are left out."},
	{"Still downloading", "Events whose video was still downloading when this report was made are counted in Events only."},
	{"Dates", "Whole days in WIB; the first and last day are both included."},
}

func definitionsSection() customreport.DocSection {
	return customreport.DocSection{Title: "Definitions", Headers: []string{"Term", "Meaning"}, Rows: summaryTerms}
}

// percent is part ÷ whole as a whole percent; "—" when there is nothing to divide.
func percent(part, whole int64) string {
	if whole == 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(part)*100/float64(whole))
}

// mb matches the page: 1 MB = 1,000,000 bytes, one decimal.
func mb(bytes int64) string { return strconv.FormatFloat(float64(bytes)/1_000_000, 'f', 1, 64) }

// rangeLabel prints WIB days ("2026-11-12") as "12 Nov 2026 – 18 Nov 2026".
func rangeLabel(fromDay, toDay string) string {
	from, err1 := time.Parse("2006-01-02", fromDay)
	to, err2 := time.Parse("2006-01-02", toDay)
	if err1 != nil || err2 != nil {
		return fromDay + " – " + toDay
	}
	if from.Equal(to) {
		return from.Format("2 Jan 2006")
	}
	return from.Format("2 Jan 2006") + " – " + to.Format("2 Jan 2006")
}

// cameraTime prints last_event, camera clock labelled UTC, as "18 Nov 2026 13:42".
func cameraTime(clock string) string {
	t, err := time.Parse("2006-01-02T15:04:05+00:00", clock)
	if err != nil {
		return clock
	}
	return t.Format("2 Jan 2006 15:04")
}
