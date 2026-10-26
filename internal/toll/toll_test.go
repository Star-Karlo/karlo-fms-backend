package toll

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/routing"
)

type stubQuoter struct {
	quote *routing.TollQuote
	err   error
	got   [][2]float64
}

func (s *stubQuoter) PriceRoute(_ context.Context, g [][2]float64) (*routing.TollQuote, error) {
	s.got = g
	return s.quote, s.err
}

func serve(t *testing.T, q routing.TollQuoter, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	NewHandler(q).Routes(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/toll/quote", strings.NewReader(body)))
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) Quote {
	t.Helper()
	var q Quote
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatalf("decode: %v — body %s", err, w.Body.String())
	}
	return q
}

// ALL FIVE classes come back even when axles were supplied. 47% of the legacy
// fleet has no axle count, and collapsing to one figure would mean this
// endpoint silently picking a class on the caller's behalf.
func TestAllFiveClassesAreReturnedEvenWithAxles(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{
		ByGolongan: map[int]int64{1: 10, 2: 20, 3: 30, 4: 40, 5: 50},
	}}
	out := decode(t, serve(t, q, `{"geometry":[[110.4,-6.9],[111.9,-7.0]],"axles":3}`))

	if len(out.ByGolongan) != 5 {
		t.Errorf("returned %d classes, want 5", len(out.ByGolongan))
	}
	if out.Golongan != 3 {
		t.Errorf("golongan = %d, want 3 for three axles", out.Golongan)
	}
	if out.Amount == nil || *out.Amount != 30 {
		t.Errorf("amount = %v, want 30", out.Amount)
	}
}

// Without axles there is no class to name, and the figure must be ABSENT
// rather than zero — zero is a claim about money.
func TestWithoutAxlesThereIsNoSingleFigure(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{ByGolongan: map[int]int64{2: 20}}}
	out := decode(t, serve(t, q, `{"geometry":[[110.4,-6.9],[111.9,-7.0]]}`))

	if out.Golongan != 0 {
		t.Errorf("golongan = %d, want 0 when no axles were supplied", out.Golongan)
	}
	if out.Amount != nil {
		t.Errorf("amount = %v, want absent", *out.Amount)
	}
	if len(out.ByGolongan) == 0 {
		t.Error("the range was dropped, leaving the caller nothing at all")
	}
}

// A class the table cannot price is UNKNOWN. Returning 0 would report a free
// journey, and that figure becomes a driver's cash advance.
func TestAnUnpricedClassIsAbsentNotZero(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{ByGolongan: map[int]int64{2: 20}}}
	out := decode(t, serve(t, q, `{"geometry":[[110.4,-6.9],[111.9,-7.0]],"axles":5}`))
	if out.Amount != nil {
		t.Errorf("amount = %d for a class with no tariff, want absent", *out.Amount)
	}
}

// When a gate went unpriced the total is an UNDERSTATEMENT. A caller has to be
// able to tell a cheap route from an unmeasured one.
func TestAnUnderstatedTotalSaysSo(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{
		ByGolongan:    map[int]int64{2: 20},
		UnpricedGates: []string{"Cibitung 4"},
	}}
	out := decode(t, serve(t, q, `{"geometry":[[110.4,-6.9],[111.9,-7.0]]}`))

	if out.Complete {
		t.Error("reported complete while a gate went unpriced")
	}
	if len(out.UnpricedGates) != 1 || out.UnpricedGates[0] != "Cibitung 4" {
		t.Errorf("unpriced gates = %v, want the gate named", out.UnpricedGates)
	}
}

func TestAFullyPricedRouteIsMarkedComplete(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{ByGolongan: map[int]int64{2: 20}}}
	if !decode(t, serve(t, q, `{"geometry":[[110.4,-6.9],[111.9,-7.0]]}`)).Complete {
		t.Error("a route with no unpriced gates was not marked complete")
	}
}

// The GEOMETRY reaches the pricer untouched, in travel order. A toll is
// incurred at the gates a route passes, so a reordered or truncated line
// prices a different journey.
func TestTheGeometryReachesThePricerInOrder(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{ByGolongan: map[int]int64{}}}
	serve(t, q, `{"geometry":[[110.4,-6.9],[111.0,-6.95],[111.9,-7.0]]}`)

	if len(q.got) != 3 {
		t.Fatalf("pricer received %d points, want 3", len(q.got))
	}
	if q.got[0][0] != 110.4 || q.got[2][0] != 111.9 {
		t.Errorf("geometry arrived reordered: %v", q.got)
	}
}

// Empty tables must REFUSE to price rather than report a toll-free journey.
func TestNoPricerRefusesRatherThanReportingFree(t *testing.T) {
	w := serve(t, nil, `{"geometry":[[110.4,-6.9],[111.9,-7.0]]}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if strings.Contains(w.Body.String(), "by_golongan") {
		t.Error("answered with a quote while unable to price")
	}
}

func TestAShortGeometryIsRefused(t *testing.T) {
	q := &stubQuoter{quote: &routing.TollQuote{}}
	for _, body := range []string{`{"geometry":[]}`, `{"geometry":[[110.4,-6.9]]}`, `{}`} {
		if w := serve(t, q, body); w.Code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, w.Code)
		}
	}
}
