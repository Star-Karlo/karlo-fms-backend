package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// A long window must arrive as several bounded requests, not one unbounded
// one — the 52-day single query is what timed out in production.
func TestHistorySplitsLongWindows(t *testing.T) {
	var mu sync.Mutex
	var spans [][2]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		mu.Lock()
		spans = append(spans, [2]string{q.Get("from"), q.Get("to")})
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"points": []Position{{IMEI: "123", Time: time.Now()}}, "next_cursor": "",
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", 5*time.Second)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(52 * 24 * time.Hour)
	pts, truncated, err := c.HistoryAll(context.Background(), "123", from, to, nil, 2000)
	if err != nil {
		t.Fatalf("HistoryAll: %v", err)
	}
	if truncated {
		t.Error("a downsampled fetch is never truncated")
	}
	// 52 days in 7-day chunks.
	if want := 8; len(spans) != want {
		t.Errorf("made %d requests, want %d bounded chunks", len(spans), want)
	}
	if len(pts) != len(spans) {
		t.Errorf("got %d points from %d chunks; every chunk's points must survive the merge", len(pts), len(spans))
	}
}

// A short window must stay one request: chunking a day would turn one fast
// query into several for nothing.
func TestHistoryKeepsShortWindowsWhole(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"points": []Position{}, "next_cursor": ""})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", 5*time.Second)
	from := time.Now().Add(-24 * time.Hour)
	if _, _, err := c.HistoryAll(context.Background(), "123", from, time.Now(), nil, 2000); err != nil {
		t.Fatalf("HistoryAll: %v", err)
	}
	if calls != 1 {
		t.Errorf("made %d requests for a one-day window, want 1", calls)
	}
}

// Raw mode follows next_cursor. It used to read the first page and drop the
// cursor, so "every fix" silently meant "the first thousand".
func TestHistoryRawFollowsCursor(t *testing.T) {
	pages := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("max_points"); got != "" {
			t.Errorf("raw mode must not ask for downsampling, got max_points=%q", got)
		}
		pages++
		next := ""
		if pages < 3 {
			next = "cursor-" + string(rune('0'+pages))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"points": []Position{{IMEI: "123"}, {IMEI: "123"}}, "next_cursor": next,
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", 5*time.Second)
	from := time.Now().Add(-2 * time.Hour)
	pts, truncated, err := c.HistoryAll(context.Background(), "123", from, time.Now(), nil, 0)
	if err != nil {
		t.Fatalf("HistoryAll: %v", err)
	}
	if pages != 3 {
		t.Errorf("read %d pages, want 3 — the cursor must be followed to the end", pages)
	}
	if len(pts) != 6 {
		t.Errorf("got %d points, want 6 — every page's points must be kept", len(pts))
	}
	if truncated {
		t.Error("six points is not a truncated answer")
	}
}

// A chunk that fails fails the request. A track quietly missing a week reads
// as a truck that did not move.
func TestHistoryFailsLoudlyOnAMissingChunk(t *testing.T) {
	var mu sync.Mutex
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		fail := n == 2
		mu.Unlock()
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"points": []Position{{IMEI: "123"}}, "next_cursor": ""})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", 5*time.Second)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	if _, _, err := c.HistoryAll(context.Background(), "123", from, from.Add(21*24*time.Hour), nil, 2000); err == nil {
		t.Fatal("expected an error when one chunk fails, got a short track and no error")
	}
}
