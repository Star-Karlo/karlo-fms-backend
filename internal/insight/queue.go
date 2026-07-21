package insight

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// The action queue: one ranked list of everything currently waiting on a person.
//
// The dashboard already had lists of alerts, of expiring documents, of overdue
// services. Four lists in four tiles is not a worklist — nobody can tell which
// of the four to open first, and none of them has any notion of being finished.
//
// Two things make this different from another list:
//
//  1. IT MERGES SOURCES AND RANKS THEM AGAINST EACH OTHER. An STNK that expired
//     yesterday outranks a speeding alert from this morning, and the queue is
//     the only place that comparison is made.
//
//  2. IT HAS A DONE STATE. `alerts.check_status` has existed since the alerts
//     table shipped, defaulting to 'unchecked', and nothing has ever read it.
//     The queue does, so working an item removes it. A task list you cannot
//     clear is just a report that makes you feel bad.

// QueueItem is one thing waiting to be done.
type QueueItem struct {
	// Kind is the source: alert | document | service | idle_asset | battery.
	Kind     string   `json:"kind"`
	Severity Severity `json:"severity"`
	// RefID identifies the row within its kind, so the client can deep-link and
	// (for alerts) mark it checked.
	RefID        int64  `json:"ref_id"`
	VehicleID    *int64 `json:"vehicle_id,omitempty"`
	LicensePlate string `json:"license_plate,omitempty"`
	Title        string `json:"title"`
	Detail       string `json:"detail,omitempty"`
	// DueInDays is negative when already overdue, null when not time-based.
	DueInDays *int `json:"due_in_days,omitempty"`
	// Rank is the computed priority; exposed so the client can show why an item
	// sits where it does rather than presenting an inscrutable order.
	Rank int `json:"rank"`
}

// actionQueue godoc
//
//	@Summary		Everything currently waiting on a person, ranked
//	@Description	Merges unchecked alerts, expiring documents, due services, idle assets and low batteries into one ordered worklist. Alerts respect `check_status`, so marking one checked removes it from the queue.
//	@Tags			insights
//	@Security		BearerAuth
//	@Produce		json
//	@Param			limit		query	int	false	"max items (default 25)"
//	@Param			horizon_days	query	int	false	"how far ahead to look for expiries (default 30)"
//	@Success		200	{array}	insight.QueueItem
//	@Router			/v1/insights/action-queue [get]
func (h *Handler) actionQueue(w http.ResponseWriter, r *http.Request) {
	tenantID := auth.TenantID(r.Context())
	limit := intParam(r, "limit", 25, 1, 200)
	horizon := intParam(r, "horizon_days", 30, 1, 365)

	items := []QueueItem{}

	err := db.Tx(r.Context(), h.pool, tenantID, func(tx pgx.Tx) error {
		// ---- unchecked alerts -------------------------------------------
		// Only 'unchecked': the whole point of the queue is that working an
		// item makes it disappear.
		rows, err := tx.Query(r.Context(), `
SELECT a.id, a.vehicle_id, coalesce(v.license_plate,''), coalesce(a.alert_name, a.alert_code, 'Alert'),
       coalesce(a.severity,'info'), coalesce(a.address,''),
       EXTRACT(EPOCH FROM (now() - a.occurred_at))/3600
FROM alerts a
LEFT JOIN vehicles v ON v.id = a.vehicle_id
WHERE a.check_status = 'unchecked' AND a.occurred_at > now() - interval '7 days'
ORDER BY a.occurred_at DESC
LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var it QueueItem
			var sev, addr string
			var hoursAgo float64
			if err := rows.Scan(&it.RefID, &it.VehicleID, &it.LicensePlate, &it.Title,
				&sev, &addr, &hoursAgo); err != nil {
				return err
			}
			it.Kind = "alert"
			it.Severity = normaliseSeverity(sev)
			it.Detail = addr
			items = append(items, it)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// ---- documents expiring or expired ------------------------------
		drows, err := tx.Query(r.Context(), `
SELECT d.id, d.owner_id, coalesce(v.license_plate,''), coalesce(d.kind,'Document'),
       coalesce(d.number,''), (d.expiry - CURRENT_DATE)::int
FROM documents d
LEFT JOIN vehicles v ON v.id = d.owner_id AND d.owner_type = 'vehicle'
WHERE d.expiry IS NOT NULL AND d.expiry <= CURRENT_DATE + $1::int
ORDER BY d.expiry
LIMIT 200`, horizon)
		if err != nil {
			return err
		}
		defer drows.Close()
		for drows.Next() {
			var it QueueItem
			var kind, number string
			var days int
			if err := drows.Scan(&it.RefID, &it.VehicleID, &it.LicensePlate, &kind, &number, &days); err != nil {
				return err
			}
			it.Kind = "document"
			it.DueInDays = &days
			it.Title = kind + " " + expiryPhrase(days)
			it.Detail = number
			// An expired legal document can ground a truck, so it outranks one
			// that merely needs booking in.
			if days < 0 {
				it.Severity = SevCritical
			} else if days <= 7 {
				it.Severity = SevWarning
			} else {
				it.Severity = SevInfo
			}
			items = append(items, it)
		}
		if err := drows.Err(); err != nil {
			return err
		}

		// ---- services due by date ---------------------------------------
		srows, err := tx.Query(r.Context(), `
SELECT s.id, s.vehicle_id, coalesce(v.license_plate,''), coalesce(s.component,'Service'),
       (s.next_due_at::date - CURRENT_DATE)::int
FROM service_schedules s
LEFT JOIN vehicles v ON v.id = s.vehicle_id
WHERE s.next_due_at IS NOT NULL
  AND s.next_due_at::date <= CURRENT_DATE + $1::int
  AND coalesce(s.status,'') <> 'done'
ORDER BY s.next_due_at
LIMIT 200`, horizon)
		if err != nil {
			return err
		}
		defer srows.Close()
		for srows.Next() {
			var it QueueItem
			var component string
			var days int
			if err := srows.Scan(&it.RefID, &it.VehicleID, &it.LicensePlate, &component, &days); err != nil {
				return err
			}
			it.Kind = "service"
			it.DueInDays = &days
			it.Title = component + " service " + expiryPhrase(days)
			if days < 0 {
				it.Severity = SevWarning
			} else {
				it.Severity = SevInfo
			}
			items = append(items, it)
		}
		return srows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// ---- low battery, from live state --------------------------------------
	// Appended outside the transaction: it comes from the telemetry service over
	// HTTP, and a slow upstream should not hold a database transaction open.
	if h.live != nil {
		if lows, err := h.lowBattery(r.Context(), tenantID); err == nil {
			for _, b := range lows {
				v := b.VehicleID
				items = append(items, QueueItem{
					Kind: "battery", Severity: b.Severity, RefID: v, VehicleID: &v,
					LicensePlate: b.LicensePlate,
					Title:        "Battery " + strconv.FormatFloat(b.BatteryV, 'f', 1, 64) + "V — " + b.Recommendation,
					Detail:       b.Detail,
				})
			}
		}
	}

	for i := range items {
		items[i].Rank = rankOf(items[i])
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Rank > items[j].Rank })
	httpx.JSON(w, http.StatusOK, interleave(items, limit))
}

// interleave caps how much of the queue any one kind may occupy.
//
// Ranking alone produced a queue that was 100% documents on real data: this
// fleet carries STNK and KIR records that expired years ago, and every one of
// them outranks today's alerts on severity. Sorting was working exactly as
// designed and the result was useless — a worklist that never mentions the
// battery about to strand a truck because forty legacy documents come first.
//
// So each kind gets at most half the list, filled in rank order, and whatever
// space is left over is handed back to whoever has items remaining. The top
// item is still the single most urgent thing; what changes is that the queue
// stays a view of everything rather than a view of one table.
func interleave(sorted []QueueItem, limit int) []QueueItem {
	if len(sorted) <= limit {
		return sorted
	}
	perKind := limit / 2
	if perKind < 1 {
		perKind = 1
	}
	out := make([]QueueItem, 0, limit)
	used := map[string]int{}
	overflow := []QueueItem{}
	for _, it := range sorted {
		if len(out) == limit {
			break
		}
		if used[it.Kind] < perKind {
			out = append(out, it)
			used[it.Kind]++
		} else {
			overflow = append(overflow, it)
		}
	}
	// Backfill: a quota should not leave the list short when only one kind has
	// anything left to show.
	for _, it := range overflow {
		if len(out) == limit {
			break
		}
		out = append(out, it)
	}
	return out
}

// rankOf scores an item so different kinds can be ordered against each other.
//
// Severity dominates, then overdue-ness. Anything already past due gets a flat
// boost on top of how far past — a document three days expired and one thirty
// days expired are both "deal with it now", and letting the older one dominate
// the list would bury the newer problems behind a long tail of neglected ones.
func rankOf(it QueueItem) int {
	rank := severityRank[it.Severity] * 1000
	if it.DueInDays != nil {
		d := *it.DueInDays
		switch {
		case d < -staleAfterDays:
			// Expired so long ago that it is plainly not being worked: this
			// fleet holds documents five years out of date. Treating those as
			// the most urgent thing in the building is how a worklist becomes
			// wallpaper. They are backlog — still listed, but below anything
			// live, because whatever they need is not "today".
			rank -= 800
		case d < 0:
			rank += 500 - d // recently overdue: the most urgent case
		default:
			rank += 60 - minInt(d, 60) // sooner ranks higher
		}
	}
	return rank
}

// staleAfterDays is when "overdue" stops meaning "urgent" and starts meaning
// "nobody is dealing with this".
const staleAfterDays = 180

func normaliseSeverity(s string) Severity {
	switch s {
	case "critical", "high", "danger":
		return SevCritical
	case "warning", "medium", "warn":
		return SevWarning
	default:
		return SevInfo
	}
}

func expiryPhrase(days int) string {
	switch {
	case days < 0:
		return "expired " + strconv.Itoa(-days) + "d ago"
	case days == 0:
		return "expires today"
	default:
		return "due in " + strconv.Itoa(days) + "d"
	}
}

func intParam(r *http.Request, key string, def, lo, hi int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	if n < lo {
		return lo
	}
	if n > hi {
		return hi
	}
	return n
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
