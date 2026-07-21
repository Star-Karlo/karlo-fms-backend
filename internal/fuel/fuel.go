// Package fuel serves official refuelling points (SPBU) to the Live View map.
//
// Unlike every other table behind this API, fuel_station is SHARED REFERENCE
// DATA — no tenant_id, no RLS policy (see 0024_fuel_stations.sql). A Pertamina
// station on the Cipali is the same station for every customer. Queries still
// run inside db.Tx so the tenant GUC is set exactly as everywhere else; it
// simply has nothing to filter here. The table is read-only to fms_app, so
// there are no write endpoints — refreshes go through cmd/import-fuel.
package fuel

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// maxLimit caps one response.
//
// Sized to hold Java and Sumatra whole (~3,100 and ~1,900 stations at the time
// of writing) with room to grow, because the map deliberately loads the entire
// layer ONCE when it is switched on rather than refetching per viewport: the
// data is static reference material, so a single cached load makes panning and
// zooming instant and costs one request per session. bbox filtering is still
// supported for callers that want it.
const maxLimit = 10000

const defaultLimit = 10000

// Brands is the closed set cmd/import-fuel normalises into. Exposed so the
// client can build its filter UI from the server's list rather than keeping a
// second copy that drifts.
var Brands = []string{"pertamina", "shell", "bp", "vivo", "akr", "total", "other"}

// Station is one refuelling point.
type Station struct {
	ID    int64   `json:"id"`
	Brand string  `json:"brand"`
	Name  string  `json:"name,omitempty"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

// Handler serves the fuel-station endpoints.
type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the fuel endpoints. Read-only: any authenticated user may see
// stations, since they are public infrastructure and carry no tenant data.
func (h *Handler) Routes(r chi.Router) {
	r.Route("/v1/fuel-stations", func(fr chi.Router) {
		fr.Get("/", h.list)
		fr.Get("/brands", h.brands)
	})
}

// brands godoc
//
//	@Summary	Available fuel-station brands, with counts
//	@Tags		fuel-stations
//	@Security	BearerAuth
//	@Produce	json
//	@Success	200	{array}	fuel.BrandCount
//	@Router		/v1/fuel-stations/brands [get]
func (h *Handler) brands(w http.ResponseWriter, r *http.Request) {
	out := []BrandCount{}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(),
			`SELECT brand, count(*) FROM fuel_station GROUP BY brand ORDER BY count(*) DESC`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b BrandCount
			if err := rows.Scan(&b.Brand, &b.Count); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

// BrandCount is one brand and how many stations carry it.
type BrandCount struct {
	Brand string `json:"brand"`
	Count int64  `json:"count"`
}

// ListResponse wraps the stations with a truncation flag.
//
// `truncated` exists so a clipped result is visible rather than silently
// looking like a sparse region — the difference between "no stations here" and
// "too many to send" matters to someone planning a route.
type ListResponse struct {
	Items     []Station `json:"items"`
	Truncated bool      `json:"truncated"`
}

// list godoc
//
//	@Summary		Fuel stations, optionally within a bounding box
//	@Description	Shared reference data (OpenStreetMap, ODbL) — not tenant-scoped. Pass `bbox` as west,south,east,north in degrees to fetch only the current viewport. `truncated` is true when more stations matched than `limit` allowed; zoom in or raise the limit.
//	@Tags			fuel-stations
//	@Security		BearerAuth
//	@Produce		json
//	@Param			bbox	query		string	false	"west,south,east,north (degrees)"
//	@Param			brand	query		string	false	"comma-separated: pertamina,shell,bp,vivo,akr,total,other"
//	@Param			limit	query		int		false	"max rows (default and max 10000)"
//	@Success		200		{object}	fuel.ListResponse
//	@Failure		400		{object}	map[string]string
//	@Router			/v1/fuel-stations [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	limit := defaultLimit
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			httpx.Error(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = min(n, maxLimit)
	}

	// Conditions are assembled from FIXED SQL fragments only; every value the
	// caller supplies travels as a positional parameter. No request string ever
	// reaches the query text.
	where := []string{"true"}
	args := []any{}

	if s := q.Get("bbox"); s != "" {
		west, south, east, north, err := parseBBox(s)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, err.Error())
			return
		}
		args = append(args, west, south, east, north)
		where = append(where, fmt.Sprintf(
			"geog && ST_MakeEnvelope($%d,$%d,$%d,$%d, 4326)::geography",
			len(args)-3, len(args)-2, len(args)-1, len(args)))
	}

	if s := q.Get("brand"); strings.TrimSpace(s) != "" {
		brands := []string{}
		for _, b := range strings.Split(s, ",") {
			b = strings.ToLower(strings.TrimSpace(b))
			if b == "" {
				continue
			}
			if !validBrand(b) {
				httpx.Error(w, http.StatusBadRequest,
					fmt.Sprintf("unknown brand %q; valid: %s", b, strings.Join(Brands, ", ")))
				return
			}
			brands = append(brands, b)
		}
		// An explicitly empty selection means "show nothing", which is a legal
		// state in the map's filter UI — the same rule the coverage overlay and
		// dashboard filter chips follow.
		if len(brands) == 0 {
			httpx.JSON(w, http.StatusOK, ListResponse{Items: []Station{}})
			return
		}
		args = append(args, brands)
		where = append(where, fmt.Sprintf("brand = ANY($%d)", len(args)))
	}

	// One over the limit, so truncation is detected rather than guessed at.
	args = append(args, limit+1)
	sql := `SELECT id, brand, coalesce(name,''), lat, lon FROM fuel_station WHERE ` +
		strings.Join(where, " AND ") +
		fmt.Sprintf(" ORDER BY id LIMIT $%d", len(args))

	out := []Station{}
	err := db.Tx(r.Context(), h.pool, auth.TenantID(r.Context()), func(tx pgx.Tx) error {
		rows, err := tx.Query(r.Context(), sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s Station
			if err := rows.Scan(&s.ID, &s.Brand, &s.Name, &s.Lat, &s.Lon); err != nil {
				return err
			}
			out = append(out, s)
		}
		return rows.Err()
	})
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	truncated := len(out) > limit
	if truncated {
		out = out[:limit]
	}
	httpx.JSON(w, http.StatusOK, ListResponse{Items: out, Truncated: truncated})
}

func validBrand(b string) bool {
	for _, v := range Brands {
		if v == b {
			return true
		}
	}
	return false
}

// parseBBox reads "west,south,east,north" and rejects anything degenerate.
func parseBBox(s string) (west, south, east, north float64, err error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("bbox must be west,south,east,north")
	}
	vals := make([]float64, 4)
	for i, p := range parts {
		v, e := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if e != nil {
			return 0, 0, 0, 0, fmt.Errorf("bbox value %q is not a number", p)
		}
		vals[i] = v
	}
	west, south, east, north = vals[0], vals[1], vals[2], vals[3]
	if west >= east || south >= north {
		// Caught here rather than returned as an empty result, which would be
		// indistinguishable from a region with no stations.
		return 0, 0, 0, 0, fmt.Errorf("bbox is empty: expected west<east and south<north")
	}
	if south < -90 || north > 90 || west < -180 || east > 180 {
		return 0, 0, 0, 0, fmt.Errorf("bbox is outside valid lat/lon range")
	}
	return west, south, east, north, nil
}
