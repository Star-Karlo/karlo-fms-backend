package liveview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

func TestParseBulkParams(t *testing.T) {
	// Hot tier of 14 days ends at 2026-11-10T00:00:00Z.
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	day := "from=2026-11-23T00:00:00%2B07:00&to=2026-11-24T00:00:00%2B07:00"
	many := make([]string, maxBulkPageSize+1)
	for i := range many {
		many[i] = "T" + strconv.Itoa(i)
	}
	cases := []struct {
		name, query, wantErr string
	}{
		{"missing window", "", "RFC3339"},
		{"to before from", "from=2026-11-24T00:00:00Z&to=2026-11-23T00:00:00Z", "after from"},
		{"window over 7 days", "from=2026-11-15T00:00:00Z&to=2026-11-22T00:00:01Z", "7 days"},
		{"exactly 7 days", "from=2026-11-15T00:00:00Z&to=2026-11-22T00:00:00Z", ""},
		{"from in the archive", "from=2026-11-09T23:59:59Z&to=2026-11-10T12:00:00Z", "archived"},
		{"from at the hot boundary", "from=2026-11-10T00:00:00Z&to=2026-11-11T00:00:00Z", ""},
		{"page_size zero", day + "&page_size=0", "page_size"},
		{"page_size over the cap", day + "&page_size=101", "page_size"},
		{"page_size not a number", day + "&page_size=ten", "page_size"},
		{"cursor not a number", day + "&cursor=abc", "cursor"},
		{"negative cursor", day + "&cursor=-1", "cursor"},
		{"empty cursor is the first page", day + "&cursor=", ""},
		{"too many hull numbers", day + "&hull_numbers=" + strings.Join(many, ","), "hull_numbers"},
		{"cursor with hull numbers", day + "&cursor=5&hull_numbers=S1", "cannot be combined"},
		{"unknown field", day + "&fields=summary,evnts", `unknown field "evnts"`},
		{"error is not a field", day + "&fields=error", "unknown field"},
		{"stream", day + "&stream=true", ""},
		{"stream resumes from a cursor", day + "&stream=true&cursor=436", ""},
		{"stream not a boolean", day + "&stream=yes", "stream must be true or false"},
		{"stream with hull numbers", day + "&stream=true&hull_numbers=S1", "cannot be combined with hull_numbers"},
		{"stream with page_size", day + "&stream=true&page_size=50", "cannot be combined with page_size"},
		{"page_size without stream", day + "&stream=false&page_size=50", ""},
	}
	for _, c := range cases {
		q, _ := url.ParseQuery(c.query)
		_, err := parseBulkParams(q, now, 14)
		switch {
		case c.wantErr == "" && err != nil:
			t.Errorf("%s: unexpected error %v", c.name, err)
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("%s: error = %v, want one mentioning %q", c.name, err, c.wantErr)
		}
	}
}

func TestParseBulkParamsDefaults(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	q, _ := url.ParseQuery("from=2026-11-23T00:00:00Z&to=2026-11-24T00:00:00Z&hull_numbers=S 3487, s-3487,,S3490")
	p, err := parseBulkParams(q, now, 14)
	if err != nil {
		t.Fatal(err)
	}
	if p.pageSize != maxBulkPageSize || p.cursor != 0 || p.limit != 60 || p.idleThreshold != 120 {
		t.Errorf("defaults = page %d cursor %d limit %v idle %v", p.pageSize, p.cursor, p.limit, p.idleThreshold)
	}
	// Spellings of one hull number are asked once, under the first spelling.
	if !slices.Equal(p.hulls, []string{"S 3487", "S3490"}) {
		t.Errorf("hulls = %q", p.hulls)
	}
}

func TestParseBulkFields(t *testing.T) {
	now := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		fields string
		want   []string
	}{
		{"", nil},               // every field
		{" , ", nil},            // blank is the same as absent
		{"hull_no", []string{}}, // identity only, still trimmed
		{"events, stops,events,license_plate", []string{"events", "stops"}},
	}
	for _, c := range cases {
		q := url.Values{"from": {"2026-11-23T00:00:00Z"}, "to": {"2026-11-24T00:00:00Z"}, "fields": {c.fields}}
		p, err := parseBulkParams(q, now, 14)
		if err != nil {
			t.Fatalf("fields=%q: %v", c.fields, err)
		}
		if (p.fields == nil) != (c.want == nil) || !slices.Equal(p.fields, c.want) {
			t.Errorf("fields=%q: got %#v, want %#v", c.fields, p.fields, c.want)
		}
	}
}

// A trimmed item keeps what identifies it, its error, and nothing unasked; a
// failed one has null figures, as in the full row.
func TestSparseItems(t *testing.T) {
	ok := BulkBehaviorItem{BehaviorResult: &BehaviorResult{LicensePlate: "B 1 A", Events: []BehaviorEvent{}}}
	failed := failedItem(store.GetVehicleBehaviorConfigRow{LicensePlate: "B 2 A"}, 60, 120, "telemetry_timeout")
	got := sparseItems([]BulkBehaviorItem{ok, failed}, []string{"summary", "events"})

	keys := func(m map[string]any) []string { return slices.Sorted(maps.Keys(m)) }
	if k := keys(got[0]); !slices.Equal(k, []string{"events", "hull_no", "license_plate", "summary"}) {
		t.Errorf("ok item keys = %v", k)
	}
	if k := keys(got[1]); !slices.Equal(k, []string{"error", "events", "hull_no", "license_plate", "summary"}) {
		t.Errorf("failed item keys = %v", k)
	}
	if got[1]["summary"] != nil || got[1]["events"] != nil || got[1]["error"] != "telemetry_timeout" {
		t.Errorf("failed item = %v, want null figures and the code", got[1])
	}
	// A truck with no hull number says so with null, not by leaving it out.
	b, _ := json.Marshal(got[0])
	if !strings.Contains(string(b), `"hull_no":null`) {
		t.Errorf("hull_no missing: %s", b)
	}
}

func TestNextCursor(t *testing.T) {
	cases := []struct {
		ids      []int64
		pageSize int
		want     string
	}{
		{nil, 100, ""},
		{[]int64{4, 9}, 3, ""},
		{[]int64{4, 9, 12}, 3, "12"},
	}
	for _, c := range cases {
		if got := nextCursor(c.ids, c.pageSize); got != c.want {
			t.Errorf("nextCursor(%v, %d) = %q, want %q", c.ids, c.pageSize, got, c.want)
		}
	}
}

// peakTracker records the most calls in flight at once.
type peakTracker struct{ inFlight, peak atomic.Int32 }

func (p *peakTracker) enter() {
	n := p.inFlight.Add(1)
	for {
		old := p.peak.Load()
		if n <= old || p.peak.CompareAndSwap(old, n) {
			return
		}
	}
}

func TestRunBulkKeepsOrderAndBound(t *testing.T) {
	ids := []int64{5, 3, 9, 1, 7, 2, 8}
	var pt peakTracker
	out := runBulk(ids, make(chan struct{}, 3), func(id int64) BulkBehaviorItem {
		pt.enter()
		defer pt.inFlight.Add(-1)
		time.Sleep(time.Duration(10-id) * time.Millisecond) // finish out of order
		return BulkBehaviorItem{BehaviorResult: &BehaviorResult{LicensePlate: strconv.FormatInt(id, 10)}}
	})
	for i, id := range ids {
		if out[i].LicensePlate != strconv.FormatInt(id, 10) {
			t.Fatalf("item %d = %q, want truck %d", i, out[i].LicensePlate, id)
		}
	}
	if pt.peak.Load() > 3 {
		t.Errorf("%d trucks ran at once, want at most 3", pt.peak.Load())
	}
}

// The bound is for the endpoint, not per request: two callers at once still
// share it, because telemetry's capacity is what it protects.
func TestRunBulkSharesItsBoundAcrossRequests(t *testing.T) {
	sem := make(chan struct{}, 3)
	var pt peakTracker
	fn := func(int64) BulkBehaviorItem {
		pt.enter()
		defer pt.inFlight.Add(-1)
		time.Sleep(5 * time.Millisecond)
		return BulkBehaviorItem{}
	}
	ids := []int64{1, 2, 3, 4, 5, 6, 7, 8}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { runBulk(ids, sem, fn) })
	}
	wg.Wait()
	if pt.peak.Load() > 3 {
		t.Errorf("%d trucks ran at once across two requests, want at most 3", pt.peak.Load())
	}
}

// A computed row is exactly the by-hull response; a failed one has the same
// keys in the same order, with null figures and the code.
func TestBulkItemJSON(t *testing.T) {
	hull := "S3487"
	res := &BehaviorResult{HullNo: &hull, LicensePlate: "B 1101 XX", OverspeedLimit: 60, IdleThreshold: 120,
		Events: []BehaviorEvent{}, Stops: []IdleStop{}}
	want, _ := json.Marshal(res)
	got, err := json.Marshal(BulkBehaviorItem{BehaviorResult: res})
	if err != nil || string(got) != string(want) {
		t.Errorf("computed row = %s (err %v), want the by-hull JSON %s", got, err, want)
	}

	failed, err := json.Marshal(failedItem(store.GetVehicleBehaviorConfigRow{LicensePlate: "B 1101 XX", HullNo: &hull}, 60, 120, "telemetry_timeout"))
	if err != nil {
		t.Fatal(err)
	}
	wantFailed := `{"hull_no":"S3487","license_plate":"B 1101 XX","overspeed_limit":60,"idle_threshold_min":120,` +
		`"summary":null,"events":null,"stops":null,"fuel_ratio_kmpl":null,"fuel_tank_liters":null,"error":"telemetry_timeout"}`
	if string(failed) != wantFailed {
		t.Errorf("failed row = %s\nwant         %s", failed, wantFailed)
	}
}

// timeoutErr is a network timeout, as the HTTP client reports one.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// Callers get a code they can act on; the raw error names internal hosts and
// IMEIs.
func TestBulkErrorCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"vehicle gone", pgx.ErrNoRows, "vehicle_not_found"},
		{"telemetry refused our key", &telemetry.StatusError{Code: 401}, "telemetry_misconfigured"},
		{"forbidden, wrapped", fmt.Errorf("history: %w", &telemetry.StatusError{Code: 403}), "telemetry_misconfigured"},
		{"telemetry down behind its balancer", &telemetry.StatusError{Code: 502}, "telemetry_unavailable"},
		{"telemetry errored", &telemetry.StatusError{Code: 500}, "telemetry_unavailable"},
		{"deadline", context.DeadlineExceeded, "telemetry_timeout"},
		{"client timeout", &url.Error{Op: "Get", URL: "http://tracking.internal", Err: timeoutErr{}}, "telemetry_timeout"},
		{"connection refused", &url.Error{Op: "Get", URL: "http://tracking.internal", Err: errors.New("connection refused")}, "telemetry_unavailable"},
		{"cut-off reply", io.ErrUnexpectedEOF, "telemetry_unavailable"},
	}
	for _, c := range cases {
		if got := bulkErrorCode(c.err); got != c.want {
			t.Errorf("%s: bulkErrorCode = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFailedNames(t *testing.T) {
	hull := "T0042"
	items := []BulkBehaviorItem{
		{BehaviorResult: &BehaviorResult{LicensePlate: "LT-0001"}},
		{BehaviorResult: &BehaviorResult{HullNo: &hull, LicensePlate: "LT-0042"}, Error: "telemetry_timeout"},
		{BehaviorResult: &BehaviorResult{LicensePlate: "LT-0050"}, Error: "telemetry_unavailable"},
		{BehaviorResult: &BehaviorResult{}, Error: "vehicle_not_found"},
	}
	got := failedNames(items, []int64{1, 42, 50, 77})
	if want := []string{"T0042", "LT-0050", "vehicle 77"}; !slices.Equal(got, want) {
		t.Errorf("failedNames = %q, want %q", got, want)
	}
	if got := failedNames(items[:1], []int64{1}); got != nil {
		t.Errorf("no failures should leave failed absent, got %q", got)
	}
}
