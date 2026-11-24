package routing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func route2() [][2]float64 {
	return [][2]float64{{104.736257, -3.071093}, {105.346784, -5.405248}}
}

func TestRemoteTollCarriesEveryClass(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/toll/quote" {
			t.Errorf("wrong path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"by_golongan": map[string]int64{"1": 394500, "2": 592500, "3": 592500, "4": 850500, "5": 850500},
			"segments":    []map[string]string{{"from_gate": "Gerbang Tol Kramasan", "to_gate": "Lematang Toll Plaza"}},
			"complete":    true,
		})
	}))
	defer srv.Close()

	q, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), route2())
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	// All five, not just one. A caller without an axle count — 47% of the
	// fleet — needs the range.
	if len(q.ByGolongan) != 5 {
		t.Errorf("want 5 classes, got %d", len(q.ByGolongan))
	}
	if q.ByGolongan[5] != 850500 {
		t.Errorf("golongan V: want 850500, got %d", q.ByGolongan[5])
	}
	if len(q.Segments) != 1 || q.Segments[0].FromGate != "Gerbang Tol Kramasan" {
		t.Errorf("the per-gate charges must survive the hop, got %v", q.Segments)
	}
	if q.Provider != "maps-toll" {
		t.Errorf("provider must say where the figure came from, got %q", q.Provider)
	}
}

// AN UNPRICED GATE MUST SURVIVE. Without it a caller cannot tell a cheap
// route from an unmeasured one, and each name is also a tariff someone can go
// and look up.
func TestRemoteTollKeepsUnpricedGates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"by_golongan":    map[string]int64{"5": 100000},
			"unpriced_gates": []string{"tegineneng barat"},
			"complete":       false,
		})
	}))
	defer srv.Close()

	q, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), route2())
	if err != nil {
		t.Fatalf("a partial answer is still an answer: %v", err)
	}
	if len(q.UnpricedGates) != 1 || q.UnpricedGates[0] != "tegineneng barat" {
		t.Errorf("want the gate named, got %v", q.UnpricedGates)
	}
}

// A SERVICE THAT CANNOT PRICE MUST ERROR, NOT RETURN ZERO. Rp 0 from "the
// tariff tables are unreadable" and Rp 0 from "this road is free" are the same
// number meaning opposite things; only an error distinguishes them, and only
// an error lets the MAPID fallback engage.
func TestRemoteTollErrorsRatherThanQuotingZero(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"tables unreadable", http.StatusServiceUnavailable, `{"error":"toll pricing is not available on this deployment"}`},
		{"bad gateway", http.StatusBadGateway, `{}`},
		{"no figures for any class", http.StatusOK, `{"by_golongan":{},"complete":true}`},
		{"malformed body", http.StatusOK, `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			q, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), route2())
			if err == nil {
				t.Fatalf("want an error, got a quote: %+v", q)
			}
			if q != nil {
				t.Errorf("a failed call must return no quote at all, got %+v", q)
			}
		})
	}
}

// The 503 sentence is written to be shown to a person, so it must not be
// swallowed on the way up.
func TestRemoteTollPassesTheServiceSentenceUp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"the tariff tables are empty or unreadable"}`))
	}))
	defer srv.Close()

	_, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), route2())
	if err == nil || !strings.Contains(err.Error(), "empty or unreadable") {
		t.Errorf("want the service's own sentence, got %v", err)
	}
}

func TestRemoteTollUnconfiguredIsNotAnOutage(t *testing.T) {
	// An unset URL is a deployment choice, not a fault — cmd/api falls back to
	// the local tables. It must be distinguishable from a service that is down.
	if NewRemoteToll("", "").Configured() {
		t.Error("an empty base URL must not count as configured")
	}
	if _, err := NewRemoteToll("", "").PriceRoute(context.Background(), route2()); err != ErrNotConfigured {
		t.Errorf("want ErrNotConfigured, got %v", err)
	}
}

func TestRemoteTollRefusesATooShortGeometry(t *testing.T) {
	_, err := NewRemoteToll("http://example.invalid", "").PriceRoute(context.Background(), [][2]float64{{1, 2}})
	if err == nil {
		t.Error("one point is a caller bug, not a toll-free journey")
	}
}

// THE GATEWAY'S FALLBACK HAS TO REACH THE CALLER. The gateway asks Karlo's
// own tariffs first and MAPID automatically when they cannot price a journey,
// which is the right behaviour and also means a figure can come from an
// aggregator. Without carrying priced_by and fell_back, nothing downstream
// could tell the two apart and the fallback would be invisible outside the
// gateway's own log.
func TestRemoteTollCarriesWhoPricedIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"by_golongan":{"1":22500},"complete":true,"priced_by":"mapid","fell_back":true}`))
	}))
	defer srv.Close()

	q, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), [][2]float64{{106.8, -6.2}, {107.6, -6.9}})
	if err != nil {
		t.Fatal(err)
	}
	if q.PricedBy != "mapid" {
		t.Errorf("PricedBy = %q, want mapid", q.PricedBy)
	}
	if !q.FellBack {
		t.Error("FellBack = false, want true")
	}
	// Provider is unchanged, because TMS reads it and we do not deploy TMS.
	if q.Provider != "maps-toll" {
		t.Errorf("Provider = %q — must not change under an existing consumer", q.Provider)
	}
}

// An answer with no such fields — the toll service called directly, which is
// the rollback shape — must leave them empty rather than guessing "local".
func TestAbsentPricedByIsNotGuessed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"by_golongan":{"1":22500},"complete":true}`))
	}))
	defer srv.Close()

	q, err := NewRemoteToll(srv.URL, "").PriceRoute(context.Background(), [][2]float64{{106.8, -6.2}, {107.6, -6.9}})
	if err != nil {
		t.Fatal(err)
	}
	if q.PricedBy != "" || q.FellBack {
		t.Errorf("PricedBy=%q FellBack=%v — absent must stay absent", q.PricedBy, q.FellBack)
	}
}
