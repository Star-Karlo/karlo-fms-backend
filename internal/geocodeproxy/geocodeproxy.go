// Package geocodeproxy lets the browser ask "where is this?" and "what is
// here?" without holding a platform service token.
//
// The geocoder lives on another service and its search endpoint is token-
// gated (it fronts data this platform paid to assemble, and /reverse being
// left open is the cautionary tale next door — 36k anonymous calls a day from
// environments nobody can name). A browser cannot hold that token, so the two
// calls the map forms need are relayed here, authenticated as the signed-in
// user and sent on with FMS's own credential.
//
// WHY THE MAP FORMS NEED THEM. Placing a site by typing coordinates is how a
// depot ends up in the Java Sea: -6.95/110.46 and 110.46/-6.95 are both
// plausible. Searching for the place by name and clicking the result cannot
// produce a swapped pair, and naming the pin afterwards means the address on
// the record is the address of the circle, not whatever somebody typed.
package geocodeproxy

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Handler relays to the geocode service.
type Handler struct {
	base   string
	token  string
	client *http.Client
}

// NewHandler builds the relay. An empty base URL or token leaves the
// endpoints answering 503, which the forms treat as "type the address".
func NewHandler(baseURL, token string) *Handler {
	return &Handler{
		base:  strings.TrimRight(baseURL, "/"),
		token: token,
		// Short: this sits under someone typing in a search box, and a slow
		// answer is worse than none because they have already typed more.
		client: &http.Client{Timeout: 8 * time.Second},
	}
}

// Routes mounts the relay inside the authenticated group.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/v1/geocode/search", h.search)
	r.Get("/v1/geocode/reverse", h.reverse)
}

// search godoc
//
//	@Summary		Find a place by name, to drop a pin on it
//	@Description	Relays to the platform geocoder, which searches Indonesian road and administrative-area data — roads first, then kelurahan / kecamatan / kabupaten. Not house-number precise. Answers 503 when this deployment has no geocoder configured, which the map forms treat as "type the coordinates".
//	@Tags			geocode
//	@Security		BearerAuth
//	@Produce		json
//	@Param			q		query	string	true	"at least 3 characters"	example(Jl. Raya Bekasi, Cikarang)
//	@Param			limit	query	int		false	"max candidates (default 10)"
//	@Success		200		{array}	map[string]interface{}
//	@Failure		503		{object}	map[string]string
//	@Router			/v1/geocode/search [get]
func (h *Handler) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) < 3 {
		httpx.Error(w, http.StatusBadRequest, "q must be at least 3 characters")
		return
	}
	v := url.Values{"q": {q}}
	if l := r.URL.Query().Get("limit"); l != "" {
		v.Set("limit", l)
	}
	h.relay(w, r, "/search?"+v.Encode())
}

// reverse godoc
//
//	@Summary	Name the point a pin was dropped on
//	@Tags		geocode
//	@Security	BearerAuth
//	@Produce	json
//	@Param		lat	query	number	true	"Latitude"	example(-6.1751)
//	@Param		lon	query	number	true	"Longitude"	example(106.8272)
//	@Success	200	{object}	map[string]interface{}
//	@Failure	503	{object}	map[string]string
//	@Router		/v1/geocode/reverse [get]
func (h *Handler) reverse(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, lon := q.Get("lat"), q.Get("lon")
	if lat == "" || lon == "" {
		httpx.Error(w, http.StatusBadRequest, "lat and lon are required")
		return
	}
	h.relay(w, r, "/reverse?"+url.Values{"lat": {lat}, "lon": {lon}}.Encode())
}

func (h *Handler) relay(w http.ResponseWriter, r *http.Request, path string) {
	if h.base == "" || h.token == "" {
		httpx.Error(w, http.StatusServiceUnavailable, "no geocoder is configured on this deployment")
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, h.base+path, nil)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := h.client.Do(req)
	if err != nil {
		httpx.Error(w, http.StatusBadGateway, "geocoder unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	// Passed through verbatim: the geocoder's shape is the contract, and
	// re-describing it here would be a second place to keep in step.
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 1<<20))
}
