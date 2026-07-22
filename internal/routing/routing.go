// Package routing computes driving routes — currently "get this truck to the
// hangar it is booked into".
//
// PROVIDER-AGNOSTIC ON PURPOSE. The Router interface exists because the
// provider is expected to change: OpenRouteService is the right choice at
// today's volume (a handful of routes a day, no infrastructure to run), and a
// self-hosted OSRM is the right choice once volume grows or once map matching
// is needed. Swapping is then a config value, not a rewrite of the callers.
//
// WHAT OPENROUTESERVICE CANNOT DO, stated here because it is the question this
// package will be asked next. ORS offers directions, isochrones, matrix, snap
// and export — and its `snap` endpoint is POINT snapping: it moves individual
// coordinates to the nearest road, independently of each other. That is NOT map
// matching, and it is actively worse than nothing for drawing a travelled path:
// consecutive points near a highway and its service road snap to different
// roads, nothing connects them, and the result is a confident-looking line that
// never happened. Snapping a GPS trace to the road network needs a
// sequence-aware matcher — OSRM's /match or Valhalla's trace_route — so that
// feature waits for the self-hosted engine rather than being faked here.
package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Point is a WGS84 coordinate.
type Point struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Route is a computed path between two points.
type Route struct {
	DistanceM float64 `json:"distance_m"`
	DurationS float64 `json:"duration_s"`
	// Geometry is the road-following line as [lon, lat] pairs, ready to hand to
	// a map as a GeoJSON LineString.
	Geometry [][2]float64 `json:"geometry"`
	Provider string       `json:"provider"`
}

// Router computes a route between two points.
type Router interface {
	Route(ctx context.Context, from, to Point) (*Route, error)
	Name() string
}

// ErrNotConfigured is returned when no provider has been set up, so callers can
// answer 503 with something useful rather than failing opaquely.
var ErrNotConfigured = fmt.Errorf("no routing provider is configured on this deployment")

// ---------------------------------------------------------------------------
// OpenRouteService
// ---------------------------------------------------------------------------

// orsProfile is the vehicle model the route is planned for.
//
// driving-hgv, not driving-car: this is a truck fleet, and the heavy-goods
// profile respects weight limits, height restrictions and lorry bans that a car
// route would happily ignore. A route that sends a 40ft trailer down a
// residential shortcut is worse than no route, because it looks authoritative.
const orsProfile = "driving-hgv"

// ORS calls the OpenRouteService directions API.
type ORS struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewORS builds an OpenRouteService router. An empty key disables it.
func NewORS(apiKey, baseURL string) *ORS {
	if baseURL == "" {
		baseURL = "https://api.openrouteservice.org"
	}
	return &ORS{
		apiKey:  apiKey,
		baseURL: baseURL,
		// Generous but bounded: a routing call sits in a user's request path,
		// and a provider having a bad day should surface as an error rather
		// than a spinner that never resolves.
		client: &http.Client{Timeout: 20 * time.Second},
	}
}

func (o *ORS) Name() string { return "openrouteservice" }

type orsRequest struct {
	// [lon, lat] — note the order, which is the opposite of how humans say it
	// and the single most common way to get a route across the wrong ocean.
	Coordinates [][2]float64 `json:"coordinates"`
}

type orsResponse struct {
	Features []struct {
		Geometry struct {
			Coordinates [][2]float64 `json:"coordinates"`
		} `json:"geometry"`
		Properties struct {
			Summary struct {
				Distance float64 `json:"distance"`
				Duration float64 `json:"duration"`
			} `json:"summary"`
		} `json:"properties"`
	} `json:"features"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Route asks OpenRouteService for a truck route between two points.
func (o *ORS) Route(ctx context.Context, from, to Point) (*Route, error) {
	if o.apiKey == "" {
		return nil, ErrNotConfigured
	}
	body, err := json.Marshal(orsRequest{Coordinates: [][2]float64{
		{from.Lon, from.Lat},
		{to.Lon, to.Lat},
	}})
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/v2/directions/%s/geojson", o.baseURL, orsProfile)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", o.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/geo+json")

	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routing provider unreachable: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}

	var out orsResponse
	// Decoded before the status check: ORS returns its own error envelope with
	// a message far more useful than the status line, and quota exhaustion in
	// particular is a 403 whose body says exactly that.
	_ = json.Unmarshal(payload, &out)
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("routing provider: %s", out.Error.Message)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("routing quota exhausted or key rejected (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("routing provider returned HTTP %d", resp.StatusCode)
	}
	if len(out.Features) == 0 || len(out.Features[0].Geometry.Coordinates) == 0 {
		// A real outcome, not a bug: no drivable path was found. Common when a
		// truck's last fix is inside a building or on an unmapped yard track.
		return nil, fmt.Errorf("no drivable route found between these points")
	}

	f := out.Features[0]
	return &Route{
		DistanceM: f.Properties.Summary.Distance,
		DurationS: f.Properties.Summary.Duration,
		Geometry:  f.Geometry.Coordinates,
		Provider:  o.Name(),
	}, nil
}

// ---------------------------------------------------------------------------
// OSRM — the intended successor, not yet wired to a deployment.
// ---------------------------------------------------------------------------

// OSRM calls a self-hosted Open Source Routing Machine.
//
// Present so the swap is a config change rather than a migration. It also
// carries /match, which is why the move happens at all: OSRM is the smallest
// step from "route a truck to a hangar" to "snap a day's GPS trace to roads",
// and ORS cannot do the second at any price.
type OSRM struct {
	baseURL string
	client  *http.Client
}

func NewOSRM(baseURL string) *OSRM {
	return &OSRM{baseURL: baseURL, client: &http.Client{Timeout: 20 * time.Second}}
}

func (o *OSRM) Name() string { return "osrm" }

type osrmResponse struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry struct {
			Coordinates [][2]float64 `json:"coordinates"`
		} `json:"geometry"`
	} `json:"routes"`
	Message string `json:"message"`
}

func (o *OSRM) Route(ctx context.Context, from, to Point) (*Route, error) {
	if o.baseURL == "" {
		return nil, ErrNotConfigured
	}
	url := fmt.Sprintf("%s/route/v1/driving/%f,%f;%f,%f?overview=full&geometries=geojson",
		o.baseURL, from.Lon, from.Lat, to.Lon, to.Lat)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("routing provider unreachable: %w", err)
	}
	defer resp.Body.Close()

	var out osrmResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, err
	}
	if out.Code != "Ok" || len(out.Routes) == 0 {
		msg := out.Message
		if msg == "" {
			msg = out.Code
		}
		return nil, fmt.Errorf("no drivable route found (%s)", msg)
	}
	r := out.Routes[0]
	return &Route{
		DistanceM: r.Distance,
		DurationS: r.Duration,
		Geometry:  r.Geometry.Coordinates,
		Provider:  o.Name(),
	}, nil
}

// New picks a provider from configuration. Returns nil when none is set up,
// which callers surface as 503 rather than treating as a failure.
func New(provider, orsKey, orsURL, osrmURL string) Router {
	switch provider {
	case "osrm":
		if osrmURL == "" {
			return nil
		}
		return NewOSRM(osrmURL)
	case "ors", "":
		if orsKey == "" {
			return nil
		}
		return NewORS(orsKey, orsURL)
	default:
		return nil
	}
}
