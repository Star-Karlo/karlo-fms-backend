package liveview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
)

// fakePage pages a fleet of trucks with ids 1..n as trackedVehicleIDs does.
func fakePage(n, size int) func(int64) ([]int64, error) {
	return func(after int64) ([]int64, error) {
		var ids []int64
		for id := after + 1; id <= int64(n) && len(ids) < size; id++ {
			ids = append(ids, id)
		}
		return ids, nil
	}
}

// fakeCompute answers every truck, except every seventh, which fails.
func fakeCompute(ids []int64) []BulkBehaviorItem {
	out := make([]BulkBehaviorItem, len(ids))
	for i, id := range ids {
		hull := fmt.Sprintf("T%04d", id)
		plate := "LT-" + strconv.FormatInt(id, 10)
		if id%7 == 0 {
			out[i] = failedItem(store.GetVehicleBehaviorConfigRow{HullNo: &hull, LicensePlate: plate}, 60, 120, "telemetry_timeout")
			continue
		}
		out[i] = BulkBehaviorItem{BehaviorResult: &BehaviorResult{HullNo: &hull, LicensePlate: plate,
			OverspeedLimit: 60, IdleThreshold: 120, Summary: BehaviorSummary{Points: int(id)},
			Events: []BehaviorEvent{}, Stops: []IdleStop{}}}
	}
	return out
}

func streamParams(pageSize int, fields []string) bulkParams {
	return bulkParams{
		from:  time.Date(2026, 11, 22, 0, 0, 0, 0, time.FixedZone("WIB", 7*3600)),
		to:    time.Date(2026, 11, 23, 0, 0, 0, 0, time.FixedZone("WIB", 7*3600)),
		limit: 60, idleThreshold: 120, pageSize: pageSize, stream: true, fields: fields,
	}
}

func compact(t *testing.T, b []byte) string {
	t.Helper()
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	return buf.String()
}

// streamed is what a caller decodes from a stream.
type streamed struct {
	Items      []json.RawMessage `json:"items"`
	Failed     []string          `json:"failed"`
	NextCursor string            `json:"next_cursor"`
	Error      string            `json:"error"`
}

func decodeStream(t *testing.T, b []byte) streamed {
	t.Helper()
	var s streamed
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("decode stream: %v\n%s", err, b)
	}
	return s
}

// A stream is byte for byte the page a caller would get if every truck fitted
// on one, with and without fields.
func TestBulkStreamMatchesOnePage(t *testing.T) {
	for _, fields := range [][]string{nil, {"summary"}} {
		p := streamParams(100, fields)
		rec := httptest.NewRecorder()
		sent, failed, err := writeBulkStream(context.Background(), rec, p, fakePage(250, 100), fakeCompute)
		if err != nil || sent != 250 {
			t.Fatalf("fields %v: sent %d, err %v", fields, sent, err)
		}

		all, _ := fakePage(250, 250)(0)
		page := BulkBehaviorResponse{From: p.from, To: p.to, Items: fakeCompute(all)}
		page.Failed = failedNames(page.Items, all)
		want := httptest.NewRecorder()
		if fields == nil {
			httpx.JSON(want, http.StatusOK, page)
		} else {
			httpx.JSON(want, http.StatusOK, bulkSparseResponse{BulkBehaviorResponse: page, Items: sparseItems(page.Items, fields)})
		}

		// Key by key: a trimmed page writes items after failed, a stream can't.
		var got, w map[string]json.RawMessage
		_ = json.Unmarshal(rec.Body.Bytes(), &got)
		_ = json.Unmarshal(want.Body.Bytes(), &w)
		if len(got) != len(w) {
			t.Errorf("fields %v: keys %d, want %d", fields, len(got), len(w))
		}
		for k := range w {
			if compact(t, got[k]) != compact(t, w[k]) {
				t.Errorf("fields %v: %q differs from one page\n got %.300s\nwant %.300s", fields, k, got[k], w[k])
			}
		}
		if len(failed) != 35 || rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/json" {
			t.Errorf("fields %v: failed %d, status %d, type %q", fields, len(failed), rec.Code, rec.Header().Get("Content-Type"))
		}
	}
}

func TestBulkStreamEmptyFleet(t *testing.T) {
	p := streamParams(100, nil)
	rec := httptest.NewRecorder()
	if _, _, err := writeBulkStream(context.Background(), rec, p, fakePage(0, 100), fakeCompute); err != nil {
		t.Fatal(err)
	}
	want := httptest.NewRecorder()
	httpx.JSON(want, http.StatusOK, BulkBehaviorResponse{From: p.from, To: p.to, Items: []BulkBehaviorItem{}})
	if got, w := compact(t, rec.Body.Bytes()), compact(t, want.Body.Bytes()); got != w {
		t.Errorf("empty fleet = %s, want %s", got, w)
	}
}

// A fault while choosing the next page closes the JSON with what was sent and
// where to continue; the raw error stays in the log. Continuing from there
// answers every remaining truck exactly once.
func TestBulkStreamInterruptedAndResumed(t *testing.T) {
	healthy := fakePage(250, 100)
	calls := 0
	failing := func(after int64) ([]int64, error) {
		if calls++; calls == 2 {
			return nil, errors.New("dial tcp 10.0.3.7:5432: connection refused")
		}
		return healthy(after)
	}
	rec := httptest.NewRecorder()
	sent, _, err := writeBulkStream(context.Background(), rec, streamParams(100, nil), failing, fakeCompute)
	first := decodeStream(t, rec.Body.Bytes())
	if err == nil || sent != 100 || len(first.Items) != 100 || first.NextCursor != "100" ||
		!strings.Contains(first.Error, "cursor=100") || len(first.Failed) != 14 {
		t.Fatalf("interrupted: err %v, sent %d, items %d, next %q, error %q, failed %d",
			err, sent, len(first.Items), first.NextCursor, first.Error, len(first.Failed))
	}
	if strings.Contains(rec.Body.String(), "10.0.3.7") {
		t.Errorf("raw error reached the caller: %s", first.Error)
	}

	p := streamParams(100, nil)
	p.cursor = 100
	rec = httptest.NewRecorder()
	if _, _, err := writeBulkStream(context.Background(), rec, p, healthy, fakeCompute); err != nil {
		t.Fatal(err)
	}
	rest := decodeStream(t, rec.Body.Bytes())
	seen := map[string]bool{}
	for _, raw := range append(first.Items, rest.Items...) {
		var it struct {
			HullNo string `json:"hull_no"`
		}
		_ = json.Unmarshal(raw, &it)
		if seen[it.HullNo] {
			t.Errorf("%s sent twice", it.HullNo)
		}
		seen[it.HullNo] = true
	}
	if len(seen) != 250 || rest.NextCursor != "" || rest.Error != "" {
		t.Errorf("after resuming: %d trucks, next %q, error %q", len(seen), rest.NextCursor, rest.Error)
	}
}

// Nothing has been sent when the first page cannot be read, so it is an
// ordinary error response.
func TestBulkStreamFirstPageError(t *testing.T) {
	rec := httptest.NewRecorder()
	page := func(int64) ([]int64, error) { return nil, errors.New("db down") }
	if _, _, err := writeBulkStream(context.Background(), rec, streamParams(100, nil), page, fakeCompute); err == nil {
		t.Fatal("want an error")
	}
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}

// Through a real server and chi's response wrapper, a stream outlives the
// server's WriteTimeout because it moves the deadline before every write.
func TestBulkStreamOutlivesWriteTimeout(t *testing.T) {
	slow := func(ids []int64) []BulkBehaviorItem {
		time.Sleep(400 * time.Millisecond)
		return fakeCompute(ids)
	}
	quiet := middleware.RequestLogger(&middleware.DefaultLogFormatter{Logger: log.New(io.Discard, "", 0), NoColor: true})
	srv := httptest.NewUnstartedServer(quiet(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, _ = writeBulkStream(r.Context(), w, streamParams(2, nil), fakePage(10, 2), slow)
	})))
	srv.Config.WriteTimeout = time.Second
	srv.Start()
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("stream cut off after %d bytes: %v", len(body), err)
	}
	if s := decodeStream(t, body); len(s.Items) != 10 {
		t.Errorf("got %d items, want 10", len(s.Items))
	}
}

// While a page is still computing, spaces keep the connection from looking
// idle; they are JSON whitespace, so the body still parses.
func TestBulkStreamKeepAlive(t *testing.T) {
	old := streamKeepAlive
	streamKeepAlive = 20 * time.Millisecond
	t.Cleanup(func() { streamKeepAlive = old })

	slow := func(ids []int64) []BulkBehaviorItem {
		time.Sleep(150 * time.Millisecond)
		return fakeCompute(ids)
	}
	rec := httptest.NewRecorder()
	if _, _, err := writeBulkStream(context.Background(), rec, streamParams(100, nil), fakePage(3, 100), slow); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.Body.String(), `"items":[ `) {
		t.Errorf("no keep-alive before the first page: %.80s", rec.Body)
	}
	if s := decodeStream(t, rec.Body.Bytes()); len(s.Items) != 3 {
		t.Errorf("got %d items, want 3", len(s.Items))
	}
}

// A caller that goes away stops the stream: no further page is fetched.
func TestBulkStreamStopsOnDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pages := 0
	page := func(after int64) ([]int64, error) {
		pages++
		return fakePage(10, 2)(after)
	}
	compute := func(ids []int64) []BulkBehaviorItem {
		cancel()
		return fakeCompute(ids)
	}
	_, _, err := writeBulkStream(ctx, httptest.NewRecorder(), streamParams(2, nil), page, compute)
	if !errors.Is(err, context.Canceled) || pages != 1 {
		t.Errorf("err %v after %d page fetches, want context.Canceled after 1", err, pages)
	}
}
