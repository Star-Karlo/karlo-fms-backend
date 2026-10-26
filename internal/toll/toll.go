// Package toll exposes Karlo's own toll pricing over HTTP.
//
// WHY IT EXISTS. The tariff tables, the gate list and the pricer have been in
// this service since migration 0058, and nothing outside it could reach them:
// the only caller was internal/maintenance/dispatch.go, pricing a truck's trip
// to a workshop. Meanwhile TMS priced every customer journey by calling MAPID
// with a metered key, for figures we already hold.
//
// This is the smallest surface that lets TMS stop doing that: one endpoint
// that prices a route geometry.
//
// WHY A GEOMETRY AND NOT TWO ENDPOINTS. A toll is incurred at the GATES a
// route passes, and two endpoints do not say which those are — the same lane
// driven by a different road is a different charge. PriceRoute walks the
// geometry against the gate list; PriceToll (endpoints) exists only for
// providers that do their own routing, and is not what this serves.
package toll

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/routing"
)

// Handler prices routes against the toll tables.
type Handler struct {
	quoter routing.TollQuoter
}

func NewHandler(q routing.TollQuoter) *Handler { return &Handler{quoter: q} }

// Routes mounts the endpoint. The caller decides the auth group.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/v1/toll/quote", h.quote)
}

// QuoteRequest is a route to price.
type QuoteRequest struct {
	// Geometry is the routed line, [lon, lat] in TRAVEL ORDER. GeoJSON order,
	// matching what every router here returns, so a caller passes a route's
	// geometry through untouched.
	Geometry [][2]float64 `json:"geometry"`

	// Axles narrows the answer to one class, when the caller knows it.
	//
	// OPTIONAL, and omitting it is a normal case rather than a lapse: 47% of
	// the legacy fleet has no axle count recorded. The response carries all
	// five classes either way — see Quote.
	Axles int `json:"axles,omitempty"`
}

// Quote is what a journey costs at the gates.
type Quote struct {
	// ByGolongan is ALL FIVE classes, always, even when axles were supplied.
	//
	// A caller with an axle count reads its own; one without shows the range
	// and says which it is. Collapsing to a single figure would mean this
	// endpoint picking a class on the caller's behalf, silently, for the 47%
	// of trucks that cannot be classified — and a toll figure becomes a
	// driver's cash advance.
	ByGolongan map[int]int64 `json:"by_golongan"`

	// Golongan is the class implied by the axles supplied, 0 when none were.
	// Amount is that class's figure, nil when the class is unknown — never
	// zero, which would read as a free journey.
	Golongan int    `json:"golongan,omitempty"`
	Amount   *int64 `json:"amount,omitempty"`

	// Segments are the individual charges in travel order. A journey is not
	// one transaction, and a disputed figure is argued about per gate.
	Segments []routing.TollSegment `json:"segments,omitempty"`

	// UnpricedGates names gates no tariff covered.
	//
	// WHEN THIS IS NON-EMPTY THE TOTALS ARE AN UNDERSTATEMENT: those gates
	// contributed nothing, so the real cost is higher by whatever they charge.
	// Carried rather than swallowed because a caller must be able to tell a
	// cheap route from an unmeasured one, which the figure alone cannot.
	UnpricedGates []string `json:"unpriced_gates,omitempty"`

	// Complete is UnpricedGates being empty, stated positively so a client
	// cannot read a missing array as "all good" by accident.
	Complete bool `json:"complete"`
}

// quote prices a route.
//
//	@Summary	Price a route's tolls against Karlo's own tariff tables
//	@Tags		toll
//	@Security	BearerAuth
//	@Accept		json
//	@Produce	json
//	@Param		body	body		toll.QuoteRequest	true	"route"
//	@Success	200		{object}	toll.Quote
//	@Failure	422		{object}	object	"no usable tariff data"
//	@Router		/v1/toll/quote [post]
func (h *Handler) quote(w http.ResponseWriter, r *http.Request) {
	if h.quoter == nil {
		// The tables are empty or unreadable. Said plainly and with a 503,
		// because this is a deployment state rather than a bad request — and
		// an empty table must refuse to price rather than report a journey as
		// toll-free.
		httpx.Error(w, http.StatusServiceUnavailable,
			"toll pricing is not available on this deployment: the tariff tables are empty or unreadable")
		return
	}

	var req QuoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "a route geometry is required")
		return
	}
	if len(req.Geometry) < 2 {
		httpx.Error(w, http.StatusBadRequest,
			"a route geometry needs at least two points, [lon, lat] in travel order")
		return
	}

	q, err := h.quoter.PriceRoute(r.Context(), req.Geometry)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, err.Error())
		return
	}

	out := Quote{
		ByGolongan:    q.ByGolongan,
		Segments:      q.Segments,
		UnpricedGates: q.UnpricedGates,
		Complete:      len(q.UnpricedGates) == 0,
	}
	if g := routing.Golongan(req.Axles); g != 0 {
		out.Golongan = g
		if amount, ok := q.ByGolongan[g]; ok {
			out.Amount = &amount
		}
		// No entry for that class leaves Amount nil. Not zero: a class the
		// table cannot price is unknown, and zero is a claim about money.
	}
	httpx.JSON(w, http.StatusOK, out)
}
