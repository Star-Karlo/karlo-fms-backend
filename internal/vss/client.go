// Package vss talks to a Gtrack VSS platform — the server the dashcams (MDVRs)
// report to. VSS owns the video; FMS owns the link between a VSS device and a
// truck, and asks VSS for short-lived URLs when someone wants to watch.
//
// THREE THINGS THE VENDOR API DOES THAT SHAPE THIS CLIENT:
//
//  1. THE TOKEN EXPIRES AFTER 30 MINUTES OF INACTIVITY and there is no refresh
//     call — you log in again. So the token is cached with an expiry and
//     re-acquired on demand, and a 10004/10023 ("not logged in" / "token
//     error") response triggers exactly one retry after a fresh login. Without
//     that retry every screen breaks after half an hour of quiet.
//
//  2. ERRORS COME BACK AS HTTP 200. The status lives in a JSON `status` field —
//     10000 is success, everything else is a failure with a human message. A
//     naive client that only checks the HTTP code treats every failure as a
//     successful empty response.
//
//  3. THE PASSWORD IS SENT AS AN MD5 HEX DIGEST, not the plaintext and not a
//     salted hash. That is the vendor's protocol and cannot be improved from
//     this side; the value is stored as a normal secret (SSM) and never logged.
package vss

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Vendor status codes worth naming. The full table is in the API PDF §8.1.
const (
	statusOK            = 10000
	statusNotLoggedIn   = 10004
	statusTokenError    = 10023
	statusDeviceOffline = 10043
)

// tokenTTL is deliberately shorter than the vendor's 30-minute idle expiry.
// Re-logging in early is cheap; discovering expiry mid-request costs a failed
// call and a retry, and the vendor clock is not ours to trust to the second.
const tokenTTL = 20 * time.Minute

// Config is the connection to a VSS deployment.
//
// BaseURL is the platform root INCLUDING scheme and port, e.g.
// "https://vss.example.com" or "http://10.0.0.5:9966". The vendor defaults are
// 9966 for HTTP and 443 for HTTPS; the documentation's 192.168.x addresses are
// examples, not a real server.
type Config struct {
	BaseURL  string
	Username string
	Password string
}

// Enabled reports whether the integration is configured. Everything else
// returns ErrNotConfigured when it is not, so a deployment without a VSS server
// answers a clear 503 rather than dialling an empty host.
func (c Config) Enabled() bool {
	return c.BaseURL != "" && c.Username != "" && c.Password != ""
}

// ErrNotConfigured is returned when no VSS server has been configured.
var ErrNotConfigured = fmt.Errorf("vss: not configured (set VSS_BASE_URL, VSS_USERNAME, VSS_PASSWORD)")

// Client is a VSS API client. Safe for concurrent use; the cached token is
// guarded and only one login runs at a time.
type Client struct {
	cfg  Config
	http *http.Client

	mu      sync.Mutex
	token   string
	pid     string // WebSocket credential, returned by the same login
	fetched time.Time
}

// New builds a client. A long timeout: VSS sits behind mobile networks and the
// device-list call over a few thousand units is not fast.
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// Enabled reports whether this client can talk to anything.
func (c *Client) Enabled() bool { return c.cfg.Enabled() }

// envelope is the shape every VSS response shares.
type envelope struct {
	Status int             `json:"status"`
	Msg    string          `json:"msg"`
	Data   json.RawMessage `json:"data"`
}

// APIError carries a vendor status code so callers can distinguish "device
// offline" (expected, show a message) from "server error" (log it).
type APIError struct {
	Status int
	Msg    string
	Path   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("vss %s: status %d: %s", e.Path, e.Status, e.Msg)
}

// Offline reports whether the failure was the device being unreachable rather
// than anything wrong with the request.
func (e *APIError) Offline() bool { return e.Status == statusDeviceOffline }

// md5Hex is the password encoding the vendor's login expects.
func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // vendor protocol, not our choice
	return hex.EncodeToString(sum[:])
}

// login acquires a fresh token + pid. Caller holds c.mu.
func (c *Client) login(ctx context.Context) error {
	body, _ := json.Marshal(map[string]string{
		"username": c.cfg.Username,
		"password": md5Hex(c.cfg.Password),
	})
	raw, err := c.postRaw(ctx, "/vss/user/apiLogin.action", body)
	if err != nil {
		return err
	}
	var out struct {
		Token string `json:"token"`
		PID   string `json:"pid"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("vss login: decode: %w", err)
	}
	if out.Token == "" {
		return fmt.Errorf("vss login: empty token")
	}
	c.token, c.pid, c.fetched = out.Token, out.PID, time.Now()
	return nil
}

// Token returns a valid API token, logging in when the cached one is stale.
//
// Exported because the video pages are iframes: the browser loads them straight
// from VSS and the token has to travel in that URL. See the note on
// PlaybackURL — the FMS side keeps the token out of persisted state and mints
// it per view.
func (c *Client) Token(ctx context.Context) (string, error) {
	if !c.cfg.Enabled() {
		return "", ErrNotConfigured
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Since(c.fetched) < tokenTTL {
		return c.token, nil
	}
	if err := c.login(ctx); err != nil {
		return "", err
	}
	return c.token, nil
}

// invalidate drops the cached token so the next call logs in again.
func (c *Client) invalidate() {
	c.mu.Lock()
	c.token, c.fetched = "", time.Time{}
	c.mu.Unlock()
}

// postRaw performs one JSON POST and unwraps the envelope, without the token
// dance — used by login itself.
func (c *Client) postRaw(ctx context.Context, path string, body []byte) (json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.cfg.BaseURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vss %s: %w", path, err)
	}
	defer resp.Body.Close()
	// Capped read: a misconfigured BaseURL pointing at something that is not
	// VSS should fail, not stream an unbounded body into memory.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("vss %s: read: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vss %s: HTTP %d", path, resp.StatusCode)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("vss %s: not a VSS response (check VSS_BASE_URL): %w", path, err)
	}
	if env.Status != statusOK {
		return nil, &APIError{Status: env.Status, Msg: env.Msg, Path: path}
	}
	return env.Data, nil
}

// post calls an authenticated endpoint, retrying once through a fresh login if
// the token had expired. `params` is merged with the token.
func (c *Client) post(ctx context.Context, path string, params map[string]any) (json.RawMessage, error) {
	if !c.cfg.Enabled() {
		return nil, ErrNotConfigured
	}
	attempt := func() (json.RawMessage, error) {
		tok, err := c.Token(ctx)
		if err != nil {
			return nil, err
		}
		merged := make(map[string]any, len(params)+1)
		for k, v := range params {
			merged[k] = v
		}
		merged["token"] = tok
		body, err := json.Marshal(merged)
		if err != nil {
			return nil, err
		}
		return c.postRaw(ctx, path, body)
	}
	data, err := attempt()
	var apiErr *APIError
	if err != nil && asAPIError(err, &apiErr) &&
		(apiErr.Status == statusNotLoggedIn || apiErr.Status == statusTokenError) {
		// The token aged out between calls. One retry, after which a repeat
		// failure is real and surfaces.
		c.invalidate()
		return attempt()
	}
	return data, err
}

func asAPIError(err error, target **APIError) bool {
	e, ok := err.(*APIError)
	if ok {
		*target = e
	}
	return ok
}

// ---- devices ---------------------------------------------------------------

// Device is one MDVR as VSS knows it.
type Device struct {
	DeviceNo    string `json:"deviceno"`
	DeviceName  string `json:"devicename"`
	DeviceType  string `json:"devicetype"`
	ChannelName string `json:"channelname"`
	// AccessMode follows the vendor's device-status table: -1 offline, 1
	// ethernet, 2 wifi, 3..6 2G/3G/4G/5G.
	AccessMode      int    `json:"accessmode"`
	VideoChannels   int    `json:"videoencodernumber"`
	LastOnlineTime  string `json:"lastonlinetime"`
	LastOfflineTime string `json:"lastofflinetime"`
	Longitude       any    `json:"longitude"`
	Latitude        any    `json:"latitude"`
}

// Online reports whether the device is reachable. -1 is offline and 0 is
// "unknown", which is not the same thing but is equally unwatchable.
func (d Device) Online() bool { return d.AccessMode > 0 }

// ListDevices returns every device the account can see, one page at a time.
// pageCount -1 asks VSS for everything; the vendor supports it and a fleet's
// device list is small enough that paging adds only failure modes.
func (c *Client) ListDevices(ctx context.Context, keyword string) ([]Device, error) {
	data, err := c.post(ctx, "/vss/vehicle/findAll.action", map[string]any{
		"pageNum": 1, "pageCount": -1, "keyword": keyword,
	})
	if err != nil {
		return nil, err
	}
	var out struct {
		DataList []Device `json:"dataList"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("vss devices: decode: %w", err)
	}
	return out.DataList, nil
}

// Status is the live online state of one device.
type Status struct {
	DeviceID        string `json:"deviceID"`
	AccessMode      int    `json:"accessMode"`
	LastOnlineTime  string `json:"lastOnlineTime"`
	LastOfflineTime string `json:"lastOfflineTime"`
}

// Online reports reachability, as with Device.
func (s Status) Online() bool { return s.AccessMode > 0 }

// DeviceStatus returns the online state of one or more devices.
//
// Uses the MULTI-device endpoint even for one id: it returns an array either
// way, whereas the single-device call returns a bare object, and having one
// decode path removes a whole class of "works for one, panics for two" bug.
func (c *Client) DeviceStatus(ctx context.Context, deviceIDs ...string) ([]Status, error) {
	if len(deviceIDs) == 0 {
		return nil, nil
	}
	data, err := c.post(ctx, "/vss/onoffline/queryMoreDeviceStatus.action", map[string]any{
		"deviceID": strings.Join(deviceIDs, ","),
	})
	if err != nil {
		return nil, err
	}
	var out []Status
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("vss status: decode: %w", err)
	}
	return out, nil
}

// ---- video URLs ------------------------------------------------------------

// LiveURL builds the embeddable live-preview page for a device.
//
// THE TOKEN TRAVELS IN THIS URL. That is the vendor's iframe contract and
// cannot be avoided while using their player. Two consequences the caller must
// respect: mint it per view rather than storing it, and understand that the
// token carries whatever privileges the VSS ACCOUNT has — with an admin
// account that includes apiVehicleControl, whose actions 6 and 8 cut a
// vehicle's oil and electrical circuits. A view-only VSS user removes that
// exposure entirely and is a one-line env change.
//
// stream: 1 main stream, 0 sub-stream. Sub is the default — a fleet screen
// showing several trucks over mobile links wants the smaller stream.
func (c *Client) LiveURL(ctx context.Context, deviceID string, channels []int, mainStream bool) (string, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return "", err
	}
	stream := "0"
	if mainStream {
		stream = "1"
	}
	q := url.Values{}
	q.Set("token", tok)
	q.Set("deviceId", deviceID)
	q.Set("chs", joinChannels(channels))
	q.Set("stream", stream)
	q.Set("wnum", "1")
	q.Set("panel", "0")
	// buffer=0 on the vendor's advice for anything phone-shaped; a 2 s buffer
	// on a live camera is a long time when you are watching an incident.
	q.Set("buffer", "0")
	return strings.TrimRight(c.cfg.BaseURL, "/") + "/vss/apiPage/RealVideo.html?" + q.Encode(), nil
}

// PlaybackURL builds the embeddable playback page for a time range.
// from/to are formatted as the vendor requires: YYYYMMDDHHmmss.
func (c *Client) PlaybackURL(ctx context.Context, deviceID string, channels []int, from, to time.Time) (string, error) {
	tok, err := c.Token(ctx)
	if err != nil {
		return "", err
	}
	const layout = "20060102150405"
	q := url.Values{}
	q.Set("token", tok)
	q.Set("deviceId", deviceID)
	q.Set("chs", joinChannels(channels))
	q.Set("st", from.Format(layout))
	q.Set("et", to.Format(layout))
	q.Set("wnum", "1")
	q.Set("panel", "0")
	q.Set("buffer", "0")
	return strings.TrimRight(c.cfg.BaseURL, "/") + "/vss/apiPage/ReplayVideo.html?" + q.Encode(), nil
}

// joinChannels renders the vendor's underscore-separated channel list. Channels
// are 1-based in VSS; an empty list means channel 1 so a caller that knows
// nothing still gets a picture.
func joinChannels(chs []int) string {
	if len(chs) == 0 {
		return "1"
	}
	parts := make([]string, 0, len(chs))
	for _, c := range chs {
		parts = append(parts, fmt.Sprint(c))
	}
	return strings.Join(parts, "_")
}
