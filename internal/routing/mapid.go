package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MAPID, reached through Karlo's own routing cache rather than directly.
//
// WHY THROUGH THE CACHE. MAPID is the routing engine TMS prices orders with,
// and it is billed per call. The cache (the routing endpoints on the geocode
// service) keys a journey by its content, so the lane a truck runs every day
// is paid for once and answered from Postgres afterwards — measured at 0.54s
// cold against 0.11s warm. Calling MAPID straight from here would bypass that,
// pay twice for the same road, and give FMS a second opinion about the
// distance between two places that TMS has already quoted to a customer.
//
// It is also the reason this provider needs no MAPID key: the key stays on the
// one service that holds it, and FMS presents its platform service token like
// any other internal caller.
//
// The upstream profile is the heavy-goods one, same as the other providers
// here — a car route down a residential shortcut is worse than no route when
// the vehicle is a 40ft trailer.
type MAPID struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewMAPID builds the cache-backed router. An empty base URL or token
// disables it, which New() reports as "no provider configured".
func NewMAPID(baseURL, token string) *MAPID {
	return &MAPID{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		// The cache answers in ~0.1s warm; cold it waits on MAPID. Generous
		// but bounded, like the other providers: this sits in a user's
		// request path and a provider having a bad day must surface as an
		// error, not as a spinner that never resolves.
		client: &http.Client{Timeout: 25 * time.Second},
	}
}

func (m *MAPID) Name() string { return "mapid (cached)" }

func (m *MAPID) configured() bool { return m != nil && m.baseURL != "" && m.token != "" }

type mapidRouteRequest struct {
	// [lon, lat] in travel order — the opposite of how people say it, and the
	// usual way to get a route across the wrong ocean.
	Points  [][2]float64 `json:"points"`
	Profile string       `json:"profile"`
}

// mapidAnswer is the cache's envelope: MAPID's own body under `payload`,
// stored and returned verbatim, plus whether this one was paid for.
type mapidAnswer struct {
	Payload struct {
		// MAPID returns metres and MILLISECONDS, and the geometry as GeoJSON
		// because the cache asks for points_encoded=false.
		Paths []struct {
			Distance float64 `json:"distance"`
			Time     int64   `json:"time"`
			Points   struct {
				Coordinates [][2]float64 `json:"coordinates"`
			} `json:"points"`
		} `json:"paths"`
	} `json:"payload"`
	Cached bool `json:"cached"`
}

// Route plans from one point to another through the cache.
func (m *MAPID) Route(ctx context.Context, from, to Point) (*Route, error) {
	if !m.configured() {
		return nil, ErrNotConfigured
	}
	body, err := json.Marshal(mapidRouteRequest{
		Points:  [][2]float64{{from.Lon, from.Lat}, {to.Lon, to.Lat}},
		Profile: "truck",
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+"/v1/route", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+m.token)

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routing cache unreachable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		// The cache's own error text is more useful than the status: it
		// distinguishes "no MAPID key on that service" from "bad points".
		return nil, fmt.Errorf("routing cache returned %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out mapidAnswer
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("routing cache sent something unreadable: %w", err)
	}
	if len(out.Payload.Paths) == 0 {
		return nil, fmt.Errorf("no route between those points")
	}
	p := out.Payload.Paths[0]
	return &Route{
		DistanceM: p.Distance,
		DurationS: float64(p.Time) / 1000, // milliseconds upstream, seconds here
		Geometry:  p.Points.Coordinates,
		Provider:  m.Name(),
	}, nil
}
