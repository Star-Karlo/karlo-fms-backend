package liveview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// The by-hull behaviour report for many trucks at once, for integrations that
// would otherwise call it once per truck.
//
// COMPUTED LIVE, A PAGE AT A TIME. Every truck still costs one telemetry
// history fetch, so the whole fleet in one response would run for minutes and
// outlive the server's write timeout. A page of at most maxBulkPageSize trucks
// keeps each request to seconds; the caller follows next_cursor for the rest.
//
// stream=true answers the whole fleet in one response of the same shape, still
// computed a page at a time: each page is written as soon as it is done, so no
// more than one page is held and the connection never goes quiet for long
// enough for the load balancer to drop it.
//
// The window is held inside telemetry's hot tier. Older history is read
// through Athena, which bills a scan of every device's data per query: one
// fleet-wide call reaching there would run thousands of those.

const (
	maxBulkPageSize = 100
	maxBulkWindow   = 7 * 24 * time.Hour
)

// BulkBehaviorItem is one truck's report: exactly the by-hull response, plus
// Error when it could not be computed.
type BulkBehaviorItem struct {
	*BehaviorResult
	// Error is set when this truck's figures could not be computed; summary,
	// events and stops are then null, and the rest of the page still comes
	// back. One of: telemetry_unavailable (a temporary problem reading its
	// GPS; retry later), telemetry_timeout (reading it took too long; retry
	// later or off-peak), telemetry_misconfigured (a problem on Karlo's side;
	// retrying will not help), vehicle_not_found (the truck was removed).
	Error string `json:"error,omitempty"`
}

// failedBehavior is a failed row on the wire: the by-hull keys in the same
// order, with null figures rather than zeros a caller could store as real.
type failedBehavior struct {
	HullNo         *string          `json:"hull_no"`
	LicensePlate   string           `json:"license_plate"`
	OverspeedLimit float64          `json:"overspeed_limit"`
	IdleThreshold  float64          `json:"idle_threshold_min"`
	Summary        *BehaviorSummary `json:"summary"`
	Events         []BehaviorEvent  `json:"events"`
	Stops          []IdleStop       `json:"stops"`
	FuelRatioKmpl  *float64         `json:"fuel_ratio_kmpl"`
	FuelTankLiters *float64         `json:"fuel_tank_liters"`
	Error          string           `json:"error"`
}

// MarshalJSON writes a computed row exactly as the by-hull endpoint does, and
// a failed one with null figures.
func (it BulkBehaviorItem) MarshalJSON() ([]byte, error) {
	if it.Error == "" {
		return json.Marshal(it.BehaviorResult)
	}
	b := it.BehaviorResult
	return json.Marshal(failedBehavior{
		HullNo: b.HullNo, LicensePlate: b.LicensePlate,
		OverspeedLimit: b.OverspeedLimit, IdleThreshold: b.IdleThreshold,
		FuelRatioKmpl: b.FuelRatioKmpl, FuelTankLiters: b.FuelTankLiters,
		Error: it.Error,
	})
}

// BulkBehaviorResponse is one page of the bulk behaviour report.
type BulkBehaviorResponse struct {
	From  time.Time          `json:"from"`
	To    time.Time          `json:"to"`
	Items []BulkBehaviorItem `json:"items"`
	// Failed names this page's trucks whose figures could not be computed (by
	// hull number, else plate, else vehicle id); their rows carry error.
	// Absent when every truck on the page was answered.
	Failed []string `json:"failed,omitempty"`
	// NotFound lists requested hull numbers that matched no vehicle.
	NotFound []string `json:"not_found,omitempty"`
	// Ambiguous maps a requested hull number to the plates of the vehicles it
	// matched, when it matched more than one; none of them is answered.
	Ambiguous map[string][]string `json:"ambiguous,omitempty"`
	// NextCursor is passed back as `cursor` for the next page. Absent on the
	// last page, and always absent when hull_numbers was given.
	NextCursor string `json:"next_cursor,omitempty"`
	// Error is set only when a stream=true response was cut short by a fault
	// on Karlo's side: items holds what was sent, and calling again with
	// stream=true and next_cursor as cursor continues after the last of them.
	Error string `json:"error,omitempty"`
}

// bulkSparseResponse is BulkBehaviorResponse with items cut to ?fields=. Its
// Items shadows the embedded one, so the JSON keeps the same shape.
type bulkSparseResponse struct {
	BulkBehaviorResponse
	Items []map[string]any `json:"items"`
}

// bulkFields is what ?fields= can choose, by JSON name. hull_no and
// license_plate are always present (and error when set): without them a row
// cannot be told apart. Choosing fields trims the response, not the work —
// every figure comes from the same track fetch.
var bulkFields = map[string]func(*BehaviorResult) any{
	"overspeed_limit":    func(b *BehaviorResult) any { return b.OverspeedLimit },
	"idle_threshold_min": func(b *BehaviorResult) any { return b.IdleThreshold },
	"summary":            func(b *BehaviorResult) any { return b.Summary },
	"events":             func(b *BehaviorResult) any { return b.Events },
	"stops":              func(b *BehaviorResult) any { return b.Stops },
	"fuel_ratio_kmpl":    func(b *BehaviorResult) any { return b.FuelRatioKmpl },
	"fuel_tank_liters":   func(b *BehaviorResult) any { return b.FuelTankLiters },
}

// bulkFieldNames is every name ?fields= accepts, sorted for error messages.
func bulkFieldNames() []string {
	return slices.Sorted(maps.Keys(bulkFields))
}

type bulkParams struct {
	from, to             time.Time
	limit, idleThreshold float64
	pageSize             int
	cursor               int64
	stream               bool
	hulls                []string
	// fields is nil for every field.
	fields []string
}

// parseBulkParams validates the query of the bulk behaviour endpoint. now and
// hotDays fix telemetry's hot-tier boundary, mirroring its coldBoundary.
func parseBulkParams(q url.Values, now time.Time, hotDays int) (bulkParams, error) {
	var p bulkParams
	from, err1 := time.Parse(time.RFC3339, q.Get("from"))
	to, err2 := time.Parse(time.RFC3339, q.Get("to"))
	if err1 != nil || err2 != nil {
		return p, errors.New("from/to must be RFC3339")
	}
	if !to.After(from) {
		return p, errors.New("to must be after from")
	}
	if to.Sub(from) > maxBulkWindow {
		return p, errors.New("the window must be 7 days or less")
	}
	hot := now.UTC().AddDate(0, 0, -hotDays).Truncate(24 * time.Hour)
	if from.Before(hot) {
		return p, errors.New("from must be on or after " + hot.Format(time.RFC3339) +
			"; older history is archived and too costly to read for a whole fleet")
	}
	p.from, p.to = from, to
	p.limit, p.idleThreshold = behaviorThresholds(q)

	p.pageSize = maxBulkPageSize
	if v := q.Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxBulkPageSize {
			return p, errors.New("page_size must be between 1 and " + strconv.Itoa(maxBulkPageSize))
		}
		p.pageSize = n
	}
	if v := q.Get("cursor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return p, errors.New("cursor must be the next_cursor of a previous page")
		}
		p.cursor = n
	}
	if v := q.Get("stream"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return p, errors.New("stream must be true or false")
		}
		p.stream = b
	}
	if p.stream && q.Get("page_size") != "" {
		return p, errors.New("stream returns every vehicle; it cannot be combined with page_size")
	}

	seen := map[string]bool{}
	for h := range strings.SplitSeq(q.Get("hull_numbers"), ",") {
		h = strings.TrimSpace(h)
		norm := NormalisePlate(h)
		if norm == "" || seen[norm] {
			continue
		}
		seen[norm] = true
		p.hulls = append(p.hulls, h)
	}
	if len(p.hulls) > maxBulkPageSize {
		return p, errors.New("at most " + strconv.Itoa(maxBulkPageSize) + " hull_numbers per request")
	}
	if len(p.hulls) > 0 && p.stream {
		return p, errors.New("stream returns every vehicle; it cannot be combined with hull_numbers")
	}
	if len(p.hulls) > 0 && p.cursor != 0 {
		return p, errors.New("cursor pages through all vehicles; it cannot be combined with hull_numbers")
	}

	// Unknown names are refused rather than dropped: a typo that silently
	// removes a field is found by whoever reads the integration's output.
	named := false
	for f := range strings.SplitSeq(q.Get("fields"), ",") {
		f = strings.TrimSpace(f)
		named = named || f != ""
		switch {
		case f == "" || f == "hull_no" || f == "license_plate" || slices.Contains(p.fields, f):
		case bulkFields[f] == nil:
			return p, errors.New("unknown field " + strconv.Quote(f) + "; valid: hull_no, license_plate, " +
				strings.Join(bulkFieldNames(), ", "))
		default:
			p.fields = append(p.fields, f)
		}
	}
	if p.fields == nil && named {
		p.fields = []string{} // only the identity fields were asked for
	}
	return p, nil
}

// sparseItems keeps, for each item, its identity, its error if any, and the
// chosen fields.
func sparseItems(items []BulkBehaviorItem, fields []string) []map[string]any {
	out := make([]map[string]any, len(items))
	for i, it := range items {
		m := map[string]any{"hull_no": it.HullNo, "license_plate": it.LicensePlate}
		for _, f := range fields {
			if it.Error != "" && (f == "summary" || f == "events" || f == "stops") {
				m[f] = nil // no figures, as in the full failed row
				continue
			}
			m[f] = bulkFields[f](it.BehaviorResult)
		}
		if it.Error != "" {
			m["error"] = it.Error
		}
		out[i] = m
	}
	return out
}

// nextCursor is the last id of a full page. A short page is the last one.
func nextCursor(ids []int64, pageSize int) string {
	if len(ids) == 0 || len(ids) < pageSize {
		return ""
	}
	return strconv.FormatInt(ids[len(ids)-1], 10)
}

// runBulk calls fn for every id, holding a slot of sem for each call, and
// returns the results in the order of ids. sem is shared by every bulk
// request: telemetry's capacity is per service, not per caller, and two
// callers each allowed eight at once already overloaded it in testing.
func runBulk(ids []int64, sem chan struct{}, fn func(int64) BulkBehaviorItem) []BulkBehaviorItem {
	out := make([]BulkBehaviorItem, len(ids))
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = fn(id)
		}()
	}
	wg.Wait()
	return out
}

// failedItem is the row for a truck whose figures could not be computed: its
// identity and the request's thresholds, with code as the error.
func failedItem(cfg store.GetVehicleBehaviorConfigRow, limit, idleThreshold float64, code string) BulkBehaviorItem {
	return BulkBehaviorItem{
		BehaviorResult: &BehaviorResult{
			HullNo: cfg.HullNo, LicensePlate: cfg.LicensePlate,
			OverspeedLimit: limit, IdleThreshold: idleThreshold,
			FuelRatioKmpl: cfg.FuelRatioKmpl, FuelTankLiters: cfg.FuelTankLiters,
		},
		Error: code,
	}
}

// bulkErrorCode says why a truck failed in terms a caller can act on. The raw
// error names internal hosts, addresses, IMEIs and upstream bodies, so it is
// logged, not returned.
func bulkErrorCode(err error) string {
	var status *telemetry.StatusError
	var netErr net.Error
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "vehicle_not_found"
	case errors.As(err, &status) && (status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden):
		return "telemetry_misconfigured"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "telemetry_timeout"
	}
	return "telemetry_unavailable"
}

// failedNames names the failed items by hull number, else plate, else id.
func failedNames(items []BulkBehaviorItem, ids []int64) []string {
	var out []string
	for i, it := range items {
		switch {
		case it.Error == "":
		case it.HullNo != nil && *it.HullNo != "":
			out = append(out, *it.HullNo)
		case it.LicensePlate != "":
			out = append(out, it.LicensePlate)
		default:
			out = append(out, "vehicle "+strconv.FormatInt(ids[i], 10))
		}
	}
	return out
}

// trackedVehicleIDs returns one page of the tenant's tracked, non-archived
// vehicles with id after afterID, in id order.
func (s *Service) trackedVehicleIDs(ctx context.Context, tenantID, afterID int64, pageSize int) ([]int64, error) {
	var ids []int64
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var err error
		ids, err = store.New(tx).ListTrackedVehicleIDsPage(ctx, store.ListTrackedVehicleIDsPageParams{
			AfterID: afterID, PageSize: int32(pageSize),
		})
		return err
	})
	return ids, err
}

// streamKeepAlive is how often a stream writes a space while a page is still
// computing: one slow truck can hold its page for the whole telemetry limit
// (55 s), and the load balancer drops a connection idle for 60 s. A var so
// tests can shorten it.
var streamKeepAlive = 20 * time.Second

// streamWriteGrace is how long each write of a stream may take. The server's
// WriteTimeout runs from the start of the request, so a stream moves the
// deadline forward before every write; it is never lifted, so a client that
// stops reading is still cut off.
const streamWriteGrace = 2 * time.Minute

// writeBulkStream writes every vehicle after p.cursor as one response in the
// shape of a page, computing p.pageSize of them at a time: page returns the
// ids after an id, compute their items. A failure to read the first page is a
// 500; a later one closes the response with next_cursor and error so the
// caller can continue. It returns how many items were sent and how many of
// them failed, and the error that ended the stream early, if any.
func writeBulkStream(ctx context.Context, w http.ResponseWriter, p bulkParams,
	page func(afterID int64) ([]int64, error), compute func(ids []int64) []BulkBehaviorItem) (sent int, failed []string, err error) {
	ids, err := page(p.cursor)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return 0, nil, err
	}

	rc := http.NewResponseController(w)
	send := func(b []byte) error {
		if err := rc.SetWriteDeadline(time.Now().Add(streamWriteGrace)); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return err
		}
		if _, err := w.Write(b); err != nil {
			return err
		}
		return rc.Flush()
	}

	from, _ := json.Marshal(p.from)
	to, _ := json.Marshal(p.to)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := send(slices.Concat([]byte(`{"from":`), from, []byte(`,"to":`), to, []byte(`,"items":[`))); err != nil {
		return 0, nil, err
	}

	var tail struct {
		Failed     []string `json:"failed,omitempty"`
		NextCursor string   `json:"next_cursor,omitempty"`
		Error      string   `json:"error,omitempty"`
	}
	var pageErr error
	tick := time.NewTicker(streamKeepAlive)
	defer tick.Stop()
	for len(ids) > 0 {
		done := make(chan []BulkBehaviorItem, 1)
		go func() { done <- compute(ids) }()
		var items []BulkBehaviorItem
	wait:
		for {
			select {
			case items = <-done:
				break wait
			case <-tick.C:
				if err := send([]byte(" ")); err != nil {
					return sent, tail.Failed, err
				}
			case <-ctx.Done():
				return sent, tail.Failed, ctx.Err()
			}
		}

		var sparse []map[string]any
		if p.fields != nil {
			sparse = sparseItems(items, p.fields)
		}
		var buf bytes.Buffer
		for i := range items {
			var row any = items[i]
			if sparse != nil {
				row = sparse[i]
			}
			b, err := json.Marshal(row)
			if err != nil {
				return sent, tail.Failed, err
			}
			if sent > 0 {
				buf.WriteByte(',')
			}
			buf.Write(b)
			sent++
		}
		if err := send(buf.Bytes()); err != nil {
			return sent, tail.Failed, err
		}
		tail.Failed = append(tail.Failed, failedNames(items, ids)...)

		if len(ids) < p.pageSize {
			break
		}
		if err := ctx.Err(); err != nil {
			return sent, tail.Failed, err
		}
		last := ids[len(ids)-1]
		if ids, pageErr = page(last); pageErr != nil {
			tail.NextCursor = strconv.FormatInt(last, 10)
			tail.Error = "stream interrupted; call again with stream=true and cursor=" + tail.NextCursor + " to continue"
			break
		}
	}

	closing := []byte("]}")
	if b, _ := json.Marshal(tail); len(b) > 2 {
		closing = slices.Concat([]byte("],"), b[1:]) // b without its opening brace
	}
	if err := send(closing); err != nil {
		return sent, tail.Failed, err
	}
	return sent, tail.Failed, pageErr
}

// behaviorBulk godoc
//
//	@Summary		Driving behaviour for many vehicles, a page at a time or the whole fleet in one response
//	@Description	The /v1/vehicles/by-hull/{hull_number}/behavior report for many trucks in one request: each item is exactly that response. Give `hull_numbers` to ask for specific trucks (up to 100, no paging; unknown ones are listed in `not_found`, shared ones in `ambiguous`). Leave it out for every tracked vehicle in the account, up to `page_size` per page in id order: pass `next_cursor` from the previous response as `cursor` to get the next page; a response without `next_cursor` is the last page. A truck whose figures could not be computed still has a row, with `error` set to a code (telemetry_unavailable, telemetry_timeout, telemetry_misconfigured, vehicle_not_found) and `summary`, `events` and `stops` null, and is named in the page's `failed` list; the rest of the page is unaffected. Figures are computed live, so the window is limited to 7 days and to telemetry's hot history (the last 7 days by default, counted from UTC midnight, so a whole-day window can reach back about 6 days). `fields` trims each item to the named keys (hull_no and license_plate are always included, and error when set); leave it out for the full report. It makes responses smaller — events alone are most of a page — but not faster to compute. `stream=true` returns every tracked vehicle (after `cursor`, if given) in one response of the same shape, sent as it is computed; it takes as long as walking the pages (about a minute for 3,000 trucks over one day), and may carry spaces between items. Read it as follows: (1) if the body is not valid JSON or the connection failed, the stream was cut off (for example by a restart): call again; (2) if `next_cursor` is present, the stream stopped early on Karlo's side, `error` says so, and `items` holds what was sent: keep them and call again with `stream=true` and that `cursor`; (3) otherwise the response is complete. The status is 200 in cases 2 and 3.
//	@Tags			live-view
//	@Security		BearerAuth
//	@Produce		json
//	@Param			from			query		string	true	"RFC3339"
//	@Param			to				query		string	true	"RFC3339"
//	@Param			hull_numbers	query		string	false	"comma-separated hull numbers, e.g. S3487,S3490; omit for every tracked vehicle"
//	@Param			limit			query		number	false	"overspeed limit km/h (default 60)"
//	@Param			idle_threshold	query		number	false	"minutes a stop must exceed to count as idle (default 120)"
//	@Param			page_size		query		int		false	"vehicles per page, 1-100 (default 100)"
//	@Param			cursor			query		string	false	"next_cursor from the previous page; empty for the first page"
//	@Param			fields			query		string	false	"comma-separated item keys to return: summary, events, stops, overspeed_limit, idle_threshold_min, fuel_ratio_kmpl, fuel_tank_liters; omit for all"
//	@Param			stream			query		bool	false	"true: every tracked vehicle in one response; not with hull_numbers or page_size"
//	@Success		200	{object}	liveview.BulkBehaviorResponse
//	@Failure		400	{object}	map[string]string
//	@Failure		401	{object}	map[string]string
//	@Failure		500	{object}	map[string]string
//	@Router			/v1/vehicles/behavior [get]
func (h *Handler) behaviorBulk(w http.ResponseWriter, r *http.Request) {
	p, err := parseBulkParams(r.URL.Query(), time.Now(), h.hotDays)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	ctx := r.Context()
	tenantID := auth.EffectiveTenant(r)

	sem := h.bulkSem
	if sem == nil {
		sem = make(chan struct{}, 1)
	}
	compute := func(ids []int64) []BulkBehaviorItem {
		return runBulk(ids, sem, func(id int64) BulkBehaviorItem {
			res, err := h.svc.Behavior(ctx, tenantID, id, p.from, p.to, p.limit, p.idleThreshold)
			if err != nil {
				code := bulkErrorCode(err)
				slog.WarnContext(ctx, "bulk behaviour: truck failed", "vehicle_id", id, "code", code, "error", err)
				return failedItem(h.svc.behaviorConfig(ctx, tenantID, id), p.limit, p.idleThreshold, code)
			}
			return BulkBehaviorItem{BehaviorResult: res}
		})
	}

	if p.stream {
		start := time.Now()
		sent, failed, err := writeBulkStream(ctx, w, p, func(afterID int64) ([]int64, error) {
			return h.svc.trackedVehicleIDs(ctx, tenantID, afterID, p.pageSize)
		}, compute)
		if err != nil {
			slog.WarnContext(ctx, "bulk behaviour stream: interrupted", "tenant_id", tenantID, "sent", sent, "error", err)
			return
		}
		slog.InfoContext(ctx, "bulk behaviour stream: finished", "tenant_id", tenantID, "trucks", sent,
			"failed", len(failed), "duration", time.Since(start))
		return
	}

	out := BulkBehaviorResponse{From: p.from, To: p.to}
	var ids []int64
	if len(p.hulls) == 0 {
		ids, err = h.svc.trackedVehicleIDs(ctx, tenantID, p.cursor, p.pageSize)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		out.NextCursor = nextCursor(ids, p.pageSize)
	} else {
		for _, hull := range p.hulls {
			id, ambiguous, ok, err := h.svc.VehicleIDByHull(ctx, tenantID, hull)
			switch {
			case err != nil:
				httpx.Error(w, http.StatusInternalServerError, err.Error())
				return
			case !ok:
				out.NotFound = append(out.NotFound, hull)
			case len(ambiguous) > 0:
				if out.Ambiguous == nil {
					out.Ambiguous = map[string][]string{}
				}
				out.Ambiguous[hull] = ambiguous
			default:
				ids = append(ids, id)
			}
		}
	}

	out.Items = compute(ids)
	out.Failed = failedNames(out.Items, ids)
	if p.fields != nil {
		httpx.JSON(w, http.StatusOK, bulkSparseResponse{BulkBehaviorResponse: out, Items: sparseItems(out.Items, p.fields)})
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
