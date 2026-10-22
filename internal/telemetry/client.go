// Package telemetry is the HTTP client fms-backend uses to reach the telemetry
// pipeline (the ingest service / fms-tracking). It is the single integration
// point for live and historical position data — the two systems join on IMEI
// over HTTP, never via SQL.
package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	// StateSince: when the device's drive state (moving/idle/parking) last
	// changed, kept by the telemetry current-state upsert. Live reads only.
	StateSince *time.Time `json:"state_since,omitempty"`
}

// Client calls the telemetry read API.
type Client struct {
	base   string
	key    string
	hc     *http.Client
	histHC *http.Client // longer timeout for history (may hit cold/Athena)
}

// NewClient builds a telemetry client. baseURL points at the ingest service
// (e.g. http://localhost:8082 or https://fms-tracking.karlo.id); apiKey, if set,
// is sent as X-Ingest-Key.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	// History can span COLD storage (Athena over Parquet), which is legitimately
	// far slower than a live read — a multi-day range easily exceeds the snappy
	// live timeout. Give history its own, much longer ceiling so it doesn't 15s
	// time-out before Athena returns; live/latest keep the tight `timeout`.
	histTimeout := 55 * time.Second
	if timeout > histTimeout {
		histTimeout = timeout
	}
	return &Client{
		base:   strings.TrimRight(baseURL, "/"),
		key:    apiKey,
		hc:     &http.Client{Timeout: timeout},
		histHC: &http.Client{Timeout: histTimeout},
	}
}

// liveConcurrency bounds how many IMEI batches are fetched at once. Batches were
// previously fetched one after another, so a large fleet paid (N/150) sequential
// upstream round trips; fetching them concurrently collapses that to ~1 round
// trip of wall-clock for typical fleets.
const liveConcurrency = 8

// LiveMany fetches the latest position for a set of IMEIs, trimming columns via
// ?fields=. IMEIs are chunked into URL-safe batches which are fetched CONCURRENTLY
// (bounded) and concatenated in order.
func (c *Client) LiveMany(ctx context.Context, imeis, fields []string) ([]Position, error) {
	if len(imeis) == 0 {
		return nil, nil
	}
	var batches [][]string
	for start := 0; start < len(imeis); start += liveBatchSize {
		end := start + liveBatchSize
		if end > len(imeis) {
			end = len(imeis)
		}
		batches = append(batches, imeis[start:end])
	}

	results := make([][]Position, len(batches))
	errs := make([]error, len(batches))
	sem := make(chan struct{}, liveConcurrency)
	var wg sync.WaitGroup
	for i := range batches {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			q := url.Values{}
			q.Set("imeis", strings.Join(batches[i], ","))
			if len(fields) > 0 {
				q.Set("fields", strings.Join(fields, ","))
			}
			var batch []Position
			if err := c.getJSON(ctx, "/v1/live?"+q.Encode(), &batch); err != nil {
				errs[i] = err
				return
			}
			results[i] = batch
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	var all []Position
	for _, r := range results {
		all = append(all, r...)
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

// RawPageLimit is how many fixes one upstream page carries when nothing is
// being downsampled. The endpoint's own maximum.
const RawPageLimit = 10000

// MaxRawPoints bounds an undownsampled fetch. A month of 30-second fixes is
// ~87,000, so this holds a quarter of a year for one device; past it the
// answer is truncated AND SAYS SO, rather than quietly becoming a prefix of
// the truth.
const MaxRawPoints = 400000

// History fetches a device's track over a time range.
//
// maxPoints > 0 asks the service to LTTB-downsample the whole range to about
// that many points, which comes back in one response and is what a map line
// wants. maxPoints == 0 asks for EVERY fix, which the service pages — and
// paging is why this loop exists: it used to read the first page and drop
// next_cursor, so "give me the raw track" silently returned the first 1,000
// fixes of however long a window was asked for, with nothing to say the rest
// had been left behind.
//
// The bool reports whether the answer was cut short at MaxRawPoints.
func (c *Client) History(ctx context.Context, imei string, from, to time.Time, fields []string, maxPoints int) ([]Position, error) {
	pts, _, err := c.HistoryAll(ctx, imei, from, to, fields, maxPoints)
	return pts, err
}

// HistoryAll is History, also reporting whether the result was truncated.
func (c *Client) HistoryAll(ctx context.Context, imei string, from, to time.Time, fields []string, maxPoints int) ([]Position, bool, error) {
	base := url.Values{}
	base.Set("from", from.UTC().Format(time.RFC3339))
	base.Set("to", to.UTC().Format(time.RFC3339))
	if len(fields) > 0 {
		base.Set("fields", strings.Join(fields, ","))
	}
	if maxPoints > 0 {
		base.Set("max_points", fmt.Sprintf("%d", maxPoints))
	} else {
		base.Set("limit", fmt.Sprintf("%d", RawPageLimit))
	}

	var out []Position
	cursor := ""
	for {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		// The history endpoint returns {"points":[...],"next_cursor":...}.
		var page struct {
			Points     []Position `json:"points"`
			NextCursor string     `json:"next_cursor"`
		}
		if err := c.getJSONSlow(ctx, "/v1/history/"+url.PathEscape(imei)+"?"+q.Encode(), &page); err != nil {
			return nil, false, err
		}
		out = append(out, page.Points...)
		// Downsampling bypasses pagination upstream, so one page is the whole
		// answer and a cursor would loop forever over the same range.
		if maxPoints > 0 || page.NextCursor == "" || len(page.Points) == 0 {
			return out, false, nil
		}
		if len(out) >= MaxRawPoints {
			return out, true, nil
		}
		cursor = page.NextCursor
	}
}

// DashcamTrackPoint is one GPS fix from a dashcam's own GPS — the history
// source for a vehicle that has a camera but no tracker.
type DashcamTrackPoint struct {
	Lat  float64   `json:"lat"`
	Lon  float64   `json:"lon"`
	Time time.Time `json:"time"`
}

// DashcamEvidenceClip is one alarm with a ready evidence clip.
type DashcamEvidenceClip struct {
	Code    int       `json:"code"`
	Time    time.Time `json:"time"`
	Lat     *float64  `json:"lat"`
	Lon     *float64  `json:"lon"`
	Channel int       `json:"channel"`
}

// DashcamEvidence lists a device's evidence-backed alarms for a date (real time).
func (c *Client) DashcamEvidence(ctx context.Context, deviceID, date string) ([]DashcamEvidenceClip, error) {
	q := url.Values{}
	q.Set("date", date)
	var out []DashcamEvidenceClip
	if err := c.getJSON(ctx, "/v1/dashcams/"+url.PathEscape(deviceID)+"/evidence?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DashcamTrack fetches a dashcam device's GPS track over a range.
func (c *Client) DashcamTrack(ctx context.Context, deviceID string, from, to time.Time) ([]DashcamTrackPoint, error) {
	q := url.Values{}
	q.Set("from", from.UTC().Format(time.RFC3339))
	q.Set("to", to.UTC().Format(time.RFC3339))
	var out []DashcamTrackPoint
	if err := c.getJSON(ctx, "/v1/dashcams/"+url.PathEscape(deviceID)+"/track?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// VehicleDay is one device's rolled-up day from the tracking service.
type VehicleDay struct {
	IMEI       string   `json:"imei"`
	Day        string   `json:"day"`
	MovingS    int64    `json:"moving_s"`
	IdleS      int64    `json:"idle_s"`
	ParkingS   int64    `json:"parking_s"`
	DistanceKm float64  `json:"distance_km"`
	MaxSpeed   *float64 `json:"max_speed"`
	Samples    int64    `json:"samples"`
}

// DailyStats fetches the pre-aggregated daily rollup for every device.
//
// Deliberately unfiltered by IMEI: the tracking service holds ~2k rows per day
// and the caller drops what its tenant cannot see anyway, so one cached call
// beats a URL carrying two thousand IMEIs.
func (c *Client) DailyStats(ctx context.Context, from, to string) ([]VehicleDay, error) {
	var out []VehicleDay
	err := c.getJSON(ctx, fmt.Sprintf("/v1/stats/daily?from=%s&to=%s", url.QueryEscape(from), url.QueryEscape(to)), &out)
	return out, err
}

// Coverage proxies the ingest service's /v1/coverage grid (raw GeoJSON), passing
// the query string (metric, provider, bbox) straight through.
func (c *Client) Coverage(ctx context.Context, rawQuery string) (json.RawMessage, error) {
	path := "/v1/coverage"
	if rawQuery != "" {
		path += "?" + rawQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.key != "" {
		req.Header.Set("X-Ingest-Key", c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("telemetry %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(resp.Body)
}

func (c *Client) getJSON(ctx context.Context, path string, dst any) error {
	return c.getJSONVia(ctx, c.hc, path, dst)
}

// getJSONSlow is getJSON on the longer-timeout client, for reads that can touch
// cold storage (history over Athena/Parquet).
func (c *Client) getJSONSlow(ctx context.Context, path string, dst any) error {
	return c.getJSONVia(ctx, c.histHC, path, dst)
}

func (c *Client) getJSONVia(ctx context.Context, hc *http.Client, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.key != "" {
		req.Header.Set("X-Ingest-Key", c.key)
	}
	resp, err := hc.Do(req)
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

// ---- dashcams (fronted by the telemetry service, which talks to VSS) --------
//
// FMS never calls VSS. These methods call the telemetry service's /v1/dashcams
// endpoints; the service holds the VSS credentials and does the video work.

// MobilePosition is a driver's latest phone (K-Trip) fix, keyed by the
// master-data driver id rather than an IMEI.
type MobilePosition struct {
	Driver     string     `json:"driver"`
	Time       time.Time  `json:"time"`
	ReceivedAt *time.Time `json:"received_at"`
	Lat        *float64   `json:"lat"`
	Lon        *float64   `json:"lon"`
	Speed      *float64   `json:"speed"`
	Bearing    *float64   `json:"bearing"`
	Kecamatan  string     `json:"kecamatan"`
	Kelurahan  string     `json:"kelurahan"`
	Kabupaten  string     `json:"kabupaten"`
	Kota       string     `json:"kota"`
	Provinsi   string     `json:"provinsi"`
	Jalan      string     `json:"jalan"`
}

// MobileLiveMany returns the latest phone fix for each of the given drivers
// that has one.
func (c *Client) MobileLiveMany(ctx context.Context, drivers []string) ([]MobilePosition, error) {
	if len(drivers) == 0 {
		return nil, nil
	}
	var out []MobilePosition
	return out, c.getJSON(ctx, "/v1/mobile/live?drivers="+url.QueryEscape(strings.Join(drivers, ",")), &out)
}

// Dashcam is a device as VSS knows it, relayed by the telemetry service.
type Dashcam struct {
	DeviceID     string   `json:"device_id"`
	DeviceName   string   `json:"device_name"`
	DeviceType   string   `json:"device_type"`
	Channels     int      `json:"channels"`
	ChannelNames string   `json:"channel_names"`
	Online       bool     `json:"online"`
	AccessMode   int      `json:"access_mode"`
	Lat          *float64 `json:"lat"` // camera's own GPS, last known (nil = no fix)
	Lon          *float64 `json:"lon"`
}

// DashcamClip is one SD-card clip with its derived coordinate.
type DashcamClip struct {
	Channel  int      `json:"channel"`
	Start    string   `json:"start"`
	Stop     string   `json:"stop"`
	Duration int      `json:"duration"`
	Size     int64    `json:"size"`
	Path     string   `json:"path"`
	Lat      *float64 `json:"lat"`
	Lon      *float64 `json:"lon"`
}

// Dashcams lists devices the camera platform knows about.
func (c *Client) Dashcams(ctx context.Context, q string) ([]Dashcam, error) {
	path := "/v1/dashcams"
	if q != "" {
		path += "?q=" + url.QueryEscape(q)
	}
	var out []Dashcam
	return out, c.getJSON(ctx, path, &out)
}

// DashcamVideos lists a device's clips for a date (YYYY-MM-DD).
func (c *Client) DashcamVideos(ctx context.Context, deviceID, date, channel string) ([]DashcamClip, error) {
	q := url.Values{}
	q.Set("date", date)
	if channel != "" {
		q.Set("channel", channel)
	}
	var out []DashcamClip
	return out, c.getJSON(ctx, "/v1/dashcams/"+url.PathEscape(deviceID)+"/videos?"+q.Encode(), &out)
}

// DashcamLiveURL returns the embeddable live-view URL for a device.
func (c *Client) DashcamLiveURL(ctx context.Context, deviceID, channel, stream string) (string, error) {
	q := url.Values{}
	if channel != "" {
		q.Set("channel", channel)
	}
	if stream != "" {
		q.Set("stream", stream)
	}
	var out struct {
		URL string `json:"url"`
	}
	if err := c.getJSON(ctx, "/v1/dashcams/"+url.PathEscape(deviceID)+"/live?"+q.Encode(), &out); err != nil {
		return "", err
	}
	return out.URL, nil
}

// DashcamDownloadReq asks the telemetry service to pull one clip to S3.
type DashcamDownloadReq struct {
	Channel int    `json:"channel"`
	Path    string `json:"path"`
	Start   string `json:"start"`
	Stop    string `json:"stop"`
}

// StartDashcamDownload kicks off an async clip pull; returns the job id.
func (c *Client) StartDashcamDownload(ctx context.Context, deviceID string, req DashcamDownloadReq) (string, error) {
	var out struct {
		JobID string `json:"job_id"`
	}
	if err := c.postJSON(ctx, "/v1/dashcams/"+url.PathEscape(deviceID)+"/download", req, &out); err != nil {
		return "", err
	}
	return out.JobID, nil
}

// DashcamDownloadStatus polls a pull job. When status is "done" the s3_key is set.
type DashcamDownloadStatus struct {
	Status string `json:"status"`
	S3Key  string `json:"s3_key"`
	Bucket string `json:"bucket"`
	Error  string `json:"error"`
}

// DashcamDownloadStatus polls a download job.
func (c *Client) DashcamDownloadStatus(ctx context.Context, jobID string) (*DashcamDownloadStatus, error) {
	var out DashcamDownloadStatus
	return &out, c.getJSON(ctx, "/v1/dashcams/downloads/"+url.PathEscape(jobID), &out)
}

// postJSON POSTs a JSON body and decodes the JSON response, with the ingest key.
func (c *Client) postJSON(ctx context.Context, path string, body, dst any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("X-Ingest-Key", c.key)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("telemetry %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// DashcamAlarm is one VSS event relayed by the telemetry service.
type DashcamAlarm struct {
	ID         int64    `json:"id"`
	DeviceID   string   `json:"device_id"`
	AlarmID    string   `json:"alarm_id"`
	Code       int      `json:"code"`
	OccurredAt string   `json:"occurred_at"`
	Lat        *float64 `json:"lat"`
	Lon        *float64 `json:"lon"`
	Speed      *float64 `json:"speed"`
	// Gforce is the peak acceleration a driving-behaviour event reported, or nil.
	Gforce *float64 `json:"gforce"`
}

// DashcamAlarms returns alarms with id greater than since, id-ascending.
func (c *Client) DashcamAlarms(ctx context.Context, since int64, limit int) ([]DashcamAlarm, error) {
	q := url.Values{}
	q.Set("since", strconv.FormatInt(since, 10))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []DashcamAlarm
	return out, c.getJSON(ctx, "/v1/dashcams/alarms?"+q.Encode(), &out)
}

// GreenDrivingEvent is one tracker Green Driving event (harsh accel/brake/turn).
type GreenDrivingEvent struct {
	ID        int64  `json:"id"`
	IMEI      string `json:"imei"`
	EventType int    `json:"event_type"` // 1 accel, 2 brake, 3 corner
	// Value is the raw Teltonika Green Driving Value (device units, g×100 for the
	// accelerometer scenario). Convert to g before classifying severity.
	Value      *float64 `json:"value"`
	DurationMs *int     `json:"duration_ms"`
	OccurredAt string   `json:"occurred_at"`
	Lat        *float64 `json:"lat"`
	Lon        *float64 `json:"lon"`
	Speed      *float64 `json:"speed"`
}

// GreenDrivingEvents returns tracker Green Driving events with id greater than
// since, id-ascending — the tracker-sourced twin of DashcamAlarms.
func (c *Client) GreenDrivingEvents(ctx context.Context, since int64, limit int) ([]GreenDrivingEvent, error) {
	q := url.Values{}
	q.Set("since", strconv.FormatInt(since, 10))
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out []GreenDrivingEvent
	return out, c.getJSON(ctx, "/v1/driving-events?"+q.Encode(), &out)
}
