// Package telemetry is the HTTP client fms-backend uses to reach the telemetry
// pipeline (the ingest service / fms-tracking). It is the single integration
// point for live and historical position data — the two systems join on IMEI
// over HTTP, never via SQL.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// liveBatchSize caps how many IMEIs go into a single /v1/live?imeis= request.
// The upstream logical cap is 5000, but a GET URL with that many 15-digit IMEIs
// (~29 bytes each) blows past typical URL/header limits (e.g. an 8KB ALB limit),
// so we chunk into URL-safe batches and concatenate the results.
const liveBatchSize = 150

// Position mirrors the ingest service's read.Position 1:1 so responses unmarshal
// directly.
type Position struct {
	IMEI       string          `json:"imei"`
	Time       time.Time       `json:"time"`
	ReceivedAt *time.Time      `json:"received_at"`
	Lat        *float64        `json:"lat"`
	Lon        *float64        `json:"lon"`
	Speed      *float64        `json:"speed"`
	Bearing    *float64        `json:"bearing"`
	Ignition   *bool           `json:"ignition"`
	Kecamatan  string          `json:"kecamatan"`
	Kelurahan  string          `json:"kelurahan"`
	Kabupaten  string          `json:"kabupaten"`
	Kota       string          `json:"kota"`
	Provinsi   string          `json:"provinsi"`
	Jalan      string          `json:"jalan"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// Client calls the telemetry read API.
type Client struct {
	base string
	key  string
	hc   *http.Client
}

// NewClient builds a telemetry client. baseURL points at the ingest service
// (e.g. http://localhost:8082 or https://fms-tracking.karlo.id); apiKey, if set,
// is sent as X-Ingest-Key.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		key:  apiKey,
		hc:   &http.Client{Timeout: timeout},
	}
}

// LiveMany fetches the latest position for a set of IMEIs in one call, trimming
// columns via ?fields=. Chunks requests that exceed the upstream 5000 cap.
func (c *Client) LiveMany(ctx context.Context, imeis, fields []string) ([]Position, error) {
	if len(imeis) == 0 {
		return nil, nil
	}
	var all []Position
	for start := 0; start < len(imeis); start += liveBatchSize {
		end := start + liveBatchSize
		if end > len(imeis) {
			end = len(imeis)
		}
		q := url.Values{}
		q.Set("imeis", strings.Join(imeis[start:end], ","))
		if len(fields) > 0 {
			q.Set("fields", strings.Join(fields, ","))
		}
		var batch []Position
		if err := c.getJSON(ctx, "/v1/live?"+q.Encode(), &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)
	}
	return all, nil
}

// LiveOne fetches the latest position for a single IMEI.
func (c *Client) LiveOne(ctx context.Context, imei string) (*Position, error) {
	var p Position
	if err := c.getJSON(ctx, "/v1/live/"+url.PathEscape(imei), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// History fetches a device's track over a time range (LTTB-downsampled upstream).
func (c *Client) History(ctx context.Context, imei string, from, to time.Time, fields []string, maxPoints int) ([]Position, error) {
	q := url.Values{}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	if maxPoints > 0 {
		q.Set("max_points", fmt.Sprintf("%d", maxPoints))
	}
	// The history endpoint returns {"points":[...],"next_cursor":...}.
	var page struct {
		Points []Position `json:"points"`
	}
	if err := c.getJSON(ctx, "/v1/history/"+url.PathEscape(imei)+"?"+q.Encode(), &page); err != nil {
		return nil, err
	}
	return page.Points, nil
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.key != "" {
		req.Header.Set("X-Ingest-Key", c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telemetry %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
