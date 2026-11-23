//go:build integration

package dashcam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/dbtest"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// fleet is one test tenant with one truck and its camera.
type fleet struct {
	tenant, vehicle int64
	device          string
}

func newFleet(t *testing.T, pool *db.Pool, name string) fleet {
	t.Helper()
	tid := dbtest.Tenant(t, pool, name)
	f := fleet{tenant: tid, device: fmt.Sprintf("CAM-%d", tid)}
	f.vehicle = dbtest.QueryID(t, pool, tid,
		`INSERT INTO vehicles (tenant_id, license_plate) VALUES ($1, $2) RETURNING id`, tid, fmt.Sprintf("B %d T", tid))
	dbtest.QueryID(t, pool, tid,
		`INSERT INTO dashcams (tenant_id, vehicle_id, vss_device_id, channels) VALUES ($1, $2, $3, 2) RETURNING id`,
		tid, f.vehicle, f.device)
	return f
}

// clip inserts a video_clips row. order sets created_at, so "latest" is explicit.
func (f fleet) clip(t *testing.T, pool *db.Pool, alarmID *string, channel int, start, status string, size *int64, order int) int64 {
	t.Helper()
	return dbtest.QueryID(t, pool, f.tenant, `
INSERT INTO video_clips (tenant_id, vehicle_id, vss_device_id, channel, clip_start, status,
                         size_bytes, s3_bucket, s3_key, reason, alarm_id, created_at)
VALUES ($1, $2, $3, $4::int, $5::timestamp AT TIME ZONE 'UTC', $6::text, $7,
        CASE WHEN $6::text = 'ready' THEN 'test-bucket' END,
        CASE WHEN $6::text = 'ready' THEN 'video/' || $3 || '/' || ($8::int)::text END,
        CASE WHEN $9::text IS NULL THEN 'manual' ELSE 'auto:124' END, $9::text,
        '2026-11-12 00:00:00+00'::timestamptz + make_interval(secs => $8::int))
RETURNING id`, f.tenant, f.vehicle, f.device, channel, start, status, size, order, alarmID)
}

func request(t *testing.T, h *Handler, tenant int64, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	h.Routes(r)
	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{TenantID: tenant, Role: auth.RoleManager}))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestClipSummaryIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "summary A")
	b := newFleet(t, pool, "summary B")
	s := func(v string) *string { return &v }
	n := func(v int64) *int64 { return &v }

	// 10 Nov (WIB).
	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "failed", nil, 1)
	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "failed", nil, 2)
	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "ready", n(1_000_000), 3) // saved on the third try
	a.clip(t, pool, s("E1"), 2, "2026-11-10 08:00:00", "ready", n(4_000_000), 4) // same event, driver camera
	a.clip(t, pool, s("E2"), 1, "2026-11-10 09:00:00", "failed", nil, 5)
	a.clip(t, pool, s("E3"), 0, "2026-11-10 10:00:00", "no_clip", nil, 6)
	a.clip(t, pool, s("E4"), 1, "2026-11-10 11:00:00", "pending", nil, 7)
	a.clip(t, pool, s("E5"), 1, "2026-11-10 12:00:00", "deleted", nil, 8)
	a.clip(t, pool, nil, 1, "2026-11-10 13:00:00", "ready", n(9_000_000), 9) // manual: not counted
	// 11 Nov, 23:30 WIB: a WIB-session ::date would push it to the 12th.
	a.clip(t, pool, s("E6"), 1, "2026-11-11 23:30:00", "ready", n(500_000), 10)
	// Another customer's clip on the same day must never show up for A.
	b.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "ready", n(7_000_000), 11)
	// E2 and E3 are a second event type.
	if err := db.Tx(context.Background(), pool, a.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE video_clips SET reason = 'auto:25' WHERE alarm_id IN ('E2', 'E3')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	h := &Handler{pool: pool}
	w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips/summary?from=2026-11-10&to=2026-11-11")
	if w.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	var got ClipSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	// E1 and E6 have video, E3 none from the camera, E2 failed, E4 is still downloading.
	want := ClipTotalsDTO{Events: 5, Clips: 3, Bytes: 5_500_000, Failed: 1, NoClip: 1, Pending: 1, Trucks: 1,
		AvgEventsPerTruckDay: 2.5, AvgBytesPerTruckDay: 2_750_000, AvgBytesPerClip: 5_500_000.0 / 3,
		EventOutcomesDTO: EventOutcomesDTO{EventsWithVideo: 2, EventsCameraDidntSend: 1, EventsSaveFailed: 1}}
	if got.Totals != want {
		t.Fatalf("totals\n got %+v\nwant %+v", got.Totals, want)
	}
	wantDays := []ClipDayDTO{
		{Day: "2026-11-10", Events: 4, Clips: 2, Bytes: 5_000_000, Failed: 1, NoClip: 1},
		{Day: "2026-11-11", Events: 1, Clips: 1, Bytes: 500_000},
	}
	if len(got.Days) != 2 || got.Days[0] != wantDays[0] || got.Days[1] != wantDays[1] {
		t.Fatalf("days\n got %+v\nwant %+v", got.Days, wantDays)
	}
	c124, c25 := 124, 25
	_, n124, _ := alarmMeta(124)
	_, n25, _ := alarmMeta(25)
	wantTypes := []ClipTypeDTO{
		{Code: &c124, Name: n124, Events: 3, Clips: 3, Bytes: 5_500_000, EventOutcomesDTO: EventOutcomesDTO{EventsWithVideo: 2}},
		{Code: &c25, Name: n25, Events: 2, Failed: 1, NoClip: 1, EventOutcomesDTO: EventOutcomesDTO{EventsCameraDidntSend: 1, EventsSaveFailed: 1}},
	}
	if len(got.Types) != 2 {
		t.Fatalf("types %+v", got.Types)
	}
	for i, w := range wantTypes {
		g := got.Types[i]
		if g.Code == nil || *g.Code != *w.Code || g.Name != w.Name || g.Events != w.Events || g.Clips != w.Clips ||
			g.Bytes != w.Bytes || g.Failed != w.Failed || g.NoClip != w.NoClip || g.EventOutcomesDTO != w.EventOutcomesDTO {
			t.Fatalf("types[%d]\n got %+v\nwant %+v", i, g, w)
		}
	}
	if len(got.Trucks) != 1 || *got.Trucks[0].VehicleID != a.vehicle || got.Trucks[0].Bytes != 5_500_000 ||
		got.Trucks[0].LastEvent != "2026-11-11T23:30:00+00:00" ||
		got.Trucks[0].EventOutcomesDTO != (EventOutcomesDTO{EventsWithVideo: 2, EventsCameraDidntSend: 1, EventsSaveFailed: 1}) {
		t.Fatalf("trucks %+v", got.Trucks)
	}

	// B sees only its own clip.
	w = request(t, h, b.tenant, http.MethodGet, "/v1/video-clips/summary?from=2026-11-10&to=2026-11-11")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Totals.Clips != 1 || got.Totals.Bytes != 7_000_000 {
		t.Fatalf("tenant B totals %+v", got.Totals)
	}
}

// setReason overrides a row's reason, e.g. to make an alarm-linked SD-card pull "manual".
func (f fleet) setReason(t *testing.T, pool *db.Pool, id int64, reason string) {
	t.Helper()
	if err := db.Tx(context.Background(), pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE video_clips SET reason = $2 WHERE id = $1`, id, reason)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestClipSummaryPerAlarmIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "per alarm A")
	s := func(v string) *string { return &v }
	n := func(v int64) *int64 { return &v }

	// P1: two cameras, one saved and one failed: the event has video.
	a.clip(t, pool, s("P1"), 1, "2026-11-10 08:00:00", "ready", n(1_000_000), 1)
	a.clip(t, pool, s("P1"), 2, "2026-11-10 08:00:00", "failed", nil, 2)
	// P2: the camera sent nothing, then an SD-card pull linked to the event saved it.
	noClip := a.clip(t, pool, s("P2"), 0, "2026-11-10 09:00:00", "no_clip", nil, 3)
	a.setReason(t, pool, noClip, "auto:25")
	pull := a.clip(t, pool, s("P2"), 1, "2026-11-10 08:59:45", "ready", n(2_000_000), 4)
	a.setReason(t, pool, pull, "manual")
	// P3: the save failed, and a linked pull is still downloading.
	a.clip(t, pool, s("P3"), 1, "2026-11-10 10:00:00", "failed", nil, 5)
	pending := a.clip(t, pool, s("P3"), 1, "2026-11-10 09:59:45", "pending", nil, 6)
	a.setReason(t, pool, pending, "manual")

	h := &Handler{pool: pool}
	w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips/summary?from=2026-11-10&to=2026-11-10")
	if w.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	var got ClipSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Totals.Events != 3 || got.Totals.EventOutcomesDTO != (EventOutcomesDTO{EventsWithVideo: 2}) {
		t.Fatalf("totals %+v, want 3 events, 2 with video, P3 downloading", got.Totals)
	}
	// The pull keeps its event's type: no "manual" type, and P2 counted once under 25.
	if len(got.Types) != 2 {
		t.Fatalf("types %+v, want 124 and 25 only", got.Types)
	}
	for _, ty := range got.Types {
		switch {
		case ty.Code == nil:
			t.Fatalf("a type without a code: %+v", ty)
		case *ty.Code == 124 && (ty.Events != 2 || ty.EventsWithVideo != 1):
			t.Fatalf("type 124 %+v, want P1 and P3, one with video", ty)
		case *ty.Code == 25 && (ty.Events != 1 || ty.EventsWithVideo != 1 || ty.Bytes != 2_000_000):
			t.Fatalf("type 25 %+v, want P2 with its pulled video", ty)
		}
	}
}

func TestClipListHidesReplacedRowsIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "list replaced A")
	s := func(v string) *string { return &v }
	a.clip(t, pool, s("L1"), 0, "2026-11-10 08:00:00", "no_clip", nil, 1) // replaced by the pull below
	a.clip(t, pool, s("L1"), 1, "2026-11-10 07:59:45", "ready", nil, 2)
	a.clip(t, pool, s("L2"), 1, "2026-11-10 09:00:00", "failed", nil, 3) // an earlier attempt of a saved clip
	a.clip(t, pool, s("L2"), 1, "2026-11-10 09:00:00", "ready", nil, 4)
	a.clip(t, pool, s("L3"), 2, "2026-11-10 10:00:00", "failed", nil, 5) // another camera: still missing
	a.clip(t, pool, s("L3"), 1, "2026-11-10 10:00:00", "ready", nil, 6)
	a.clip(t, pool, s("L4"), 0, "2026-11-10 11:00:00", "no_clip", nil, 7) // nothing replaced it
	h := &Handler{pool: pool}

	list := func(q string) httpxList {
		w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips"+q)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", q, w.Code, w.Body.String())
		}
		var l httpxList
		_ = json.Unmarshal(w.Body.Bytes(), &l)
		return l
	}
	l := list("")
	if l.Total != 5 || len(l.Items) != 5 {
		t.Fatalf("default list: total %d, items %+v; want L1, L2 and L3 ready, L3's failed camera 2, L4", l.Total, l.Items)
	}
	for _, c := range l.Items {
		if (*c.AlarmID == "L1" && c.Status == "no_clip") || (*c.AlarmID == "L2" && c.Status == "failed") {
			t.Fatalf("replaced row still listed: %+v", c)
		}
	}
	if l := list("?status=failed"); l.Total != 2 {
		t.Fatalf("status=failed shows every failed row: total %d", l.Total)
	}
	if l := list("?status=no_clip"); l.Total != 2 {
		t.Fatalf("status=no_clip shows every no-clip row: total %d", l.Total)
	}
}

func TestClipSummaryExportIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "export A")
	s := func(v string) *string { return &v }
	n := func(v int64) *int64 { return &v }
	a.clip(t, pool, s("X1"), 1, "2026-11-10 08:00:00", "ready", n(1_000_000), 1)
	h := &Handler{pool: pool}

	w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips/summary/export?from=2026-11-10&to=2026-11-11&format=pdf")
	if w.Code != http.StatusOK {
		t.Fatalf("export: %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("content type %q", ct)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="saved-evidence-2026-11-10-to-2026-11-11.pdf"` {
		t.Fatalf("content disposition %q", cd)
	}
	if !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) {
		t.Fatalf("body is not a PDF: %q", w.Body.Bytes()[:min(16, w.Body.Len())])
	}
	for _, q := range []string{"?from=2026-11-10", "?from=10-11-2026&to=2026-11-11", "?from=2026-11-10&to=2026-11-11&format=csv"} {
		if w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips/summary/export"+q); w.Code != http.StatusBadRequest {
			t.Fatalf("export %s: %d, want 400", q, w.Code)
		}
	}
}

func TestDownloadLinkedToAlarmIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "pull A")
	b := newFleet(t, pool, "pull B")
	s := func(v string) *string { return &v }
	a.clip(t, pool, s("D1"), 0, "2026-11-10 08:00:00", "no_clip", nil, 1)
	a.clip(t, pool, s("D2"), 1, "2026-11-10 09:00:00", "ready", nil, 2)
	b.clip(t, pool, s("B1"), 0, "2026-11-10 08:00:00", "no_clip", nil, 3)

	var calls int
	var sent map[string]any
	tel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"job_id":"job-pull"}`))
	}))
	defer tel.Close()
	h := &Handler{pool: pool, tel: telemetry.NewClient(tel.URL, "", 5*time.Second)}
	camID := dbtest.QueryID(t, pool, a.tenant, `SELECT id FROM dashcams WHERE vss_device_id = $1`, a.device)
	pull := func(body string) *httptest.ResponseRecorder {
		r := chi.NewRouter()
		h.Routes(r)
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/v1/dashcams/%d/download", camID), strings.NewReader(body))
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{TenantID: a.tenant, Role: auth.RoleManager}))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for body, want := range map[string]int{
		`{"channel":1,"start":"2026-11-10 07:59:45","stop":"2026-11-10 08:00:15","alarm_id":"NOPE"}`: http.StatusBadRequest,
		`{"channel":1,"start":"2026-11-10 07:59:45","stop":"2026-11-10 08:00:15","alarm_id":"B1"}`:   http.StatusBadRequest, // B's event
		`{"channel":1,"start":"2026-11-10 08:59:45","stop":"2026-11-10 09:00:15","alarm_id":"D2"}`:   http.StatusConflict,
	} {
		if w := pull(body); w.Code != want {
			t.Fatalf("%s: %d %s, want %d", body, w.Code, w.Body.String(), want)
		}
	}
	if calls != 0 {
		t.Fatalf("telemetry was asked %d times for refused pulls", calls)
	}

	w := pull(`{"channel":1,"start":"2026-11-10 07:59:45","stop":"2026-11-10 08:00:15","alarm_id":"D1"}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("pull: %d %s", w.Code, w.Body.String())
	}
	if _, ok := sent["alarm_id"]; ok || calls != 1 {
		t.Fatalf("telemetry got %+v (%d calls); alarm_id must not be sent, or it skips the SD card", sent, calls)
	}
	var alarmID, reason string
	var vehicleID int64
	_ = db.Tx(context.Background(), pool, a.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(),
			`SELECT alarm_id, reason, vehicle_id FROM video_clips WHERE job_id = 'job-pull'`).Scan(&alarmID, &reason, &vehicleID)
	})
	if alarmID != "D1" || reason != "manual" || vehicleID != a.vehicle {
		t.Fatalf("pulled row: alarm %q reason %q vehicle %d", alarmID, reason, vehicleID)
	}
}

func TestClipListIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "list A")
	b := newFleet(t, pool, "list B")
	s := func(v string) *string { return &v }
	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "ready", nil, 1)
	a.clip(t, pool, s("E2"), 1, "2026-11-10 09:00:00", "deleted", nil, 2)
	a.clip(t, pool, s("E3"), 1, "2026-11-11 09:00:00", "ready", nil, 3)
	a.clip(t, pool, nil, 1, "2026-11-12 09:00:00", "ready", nil, 4)
	b.clip(t, pool, s("E9"), 1, "2026-11-10 08:00:00", "ready", nil, 5)
	h := &Handler{pool: pool}

	list := func(q string) httpxList {
		w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips"+q)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", q, w.Code, w.Body.String())
		}
		var l httpxList
		_ = json.Unmarshal(w.Body.Bytes(), &l)
		return l
	}
	if l := list(""); l.Total != 3 || len(l.Items) != 3 {
		t.Fatalf("default list hides deleted and B's clips: total %d, %d items", l.Total, len(l.Items))
	}
	if l := list("?from=2026-11-10&to=2026-11-11&limit=1&offset=1"); l.Total != 2 || len(l.Items) != 1 || *l.Items[0].AlarmID != "E1" {
		t.Fatalf("window page: %+v", l)
	}
	if l := list("?status=deleted"); l.Total != 1 || *l.Items[0].AlarmID != "E2" {
		t.Fatalf("deleted: %+v", l)
	}
	if l := list("?from=2026-11-11&to=2026-11-11"); l.Total != 1 || l.Items[0].EventCode == nil || *l.Items[0].EventCode != 124 {
		t.Fatalf("event code: %+v", l)
	}
}

type httpxList struct {
	Items []ClipDTO `json:"items"`
	Total int64     `json:"total"`
}

func TestAutoClipUniqueIndexAndDeleteIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "unique A")
	b := newFleet(t, pool, "unique B")
	ctx := context.Background()
	h := &Handler{pool: pool}
	alarmID := "E1"
	pc := pendingClip{tenantID: a.tenant, vehicleID: &a.vehicle, deviceID: a.device, channel: 1,
		start: "2026-11-10 08:00:00", stop: "2026-11-10 08:00:10", reason: "auto:124", alarmID: &alarmID}

	first, err := insertPendingClip(ctx, pool, pc, "job-1")
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if _, err := insertPendingClip(ctx, pool, pc, "job-2"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("second live row for the same event + camera: err %v, want blocked", err)
	}
	if w := request(t, h, a.tenant, http.MethodDelete, fmt.Sprintf("/v1/video-clips/%d", first)); w.Code != http.StatusConflict {
		t.Fatalf("delete while downloading: %d, want 409", w.Code)
	}

	h.failClip(ctx, a.tenant, first, "the download failed")
	retry, err := insertPendingClip(ctx, pool, pc, "job-3")
	if err != nil {
		t.Fatalf("retry after failed: %v", err)
	}
	size := int64(123)
	h.readyClip(ctx, a.tenant, retry, "test-bucket", "video/x.mp4", &size)

	if w := request(t, h, b.tenant, http.MethodDelete, fmt.Sprintf("/v1/video-clips/%d", retry)); w.Code != http.StatusNotFound {
		t.Fatalf("tenant B deleting A's clip: %d, want 404", w.Code)
	}
	if w := request(t, h, a.tenant, http.MethodDelete, fmt.Sprintf("/v1/video-clips/%d", retry)); w.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	var status string
	var key *string
	_ = db.Tx(ctx, pool, a.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT status, s3_key FROM video_clips WHERE id = $1`, retry).Scan(&status, &key)
	})
	if status != "deleted" || key != nil {
		t.Fatalf("deleted auto clip: status %q key %v, want a 'deleted' marker", status, key)
	}
	if _, err := insertPendingClip(ctx, pool, pc, "job-4"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("re-save after delete: err %v, want blocked", err)
	}
	// Only the earlier failed attempt is left for a manual Save to retry.
	if c, err := h.lookupClip(ctx, a.tenant, a.device, 1, pc.start); err != nil || c == nil || c.Status != "failed" {
		t.Fatalf("lookupClip after delete: %+v %v, want the failed attempt, never the deleted clip", c, err)
	}

	manual := a.clip(t, pool, nil, 2, "2026-11-10 09:00:00", "ready", nil, 1)
	if w := request(t, h, a.tenant, http.MethodDelete, fmt.Sprintf("/v1/video-clips/%d", manual)); w.Code != http.StatusNoContent {
		t.Fatalf("delete manual: %d", w.Code)
	}
	var left int
	_ = db.Tx(ctx, pool, a.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM video_clips WHERE id = $1`, manual).Scan(&left)
	})
	if left != 0 {
		t.Fatal("a manual clip's row should be removed")
	}
}

func TestAutosaveStoreAndLockIntegration(t *testing.T) {
	pool := dbtest.Open(t)
	a := newFleet(t, pool, "autosave A")
	b := newFleet(t, pool, "autosave B")
	ctx := context.Background()
	st := pgAutosaveStore{pool}
	s := func(v string) *string { return &v }

	b.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "ready", nil, 1)
	cams, err := st.cameras(ctx, a.tenant)
	if err != nil || len(cams) != 1 || cams[0].deviceID != a.device {
		t.Fatalf("cameras %+v %v", cams, err)
	}
	// B's camera and clip are invisible to A, even asked for by name.
	if got, _ := st.states(ctx, a.tenant, b.device, []string{"E1"}); len(got) != 0 {
		t.Fatalf("A sees B's clips: %+v", got)
	}

	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "failed", nil, 1)
	a.clip(t, pool, s("E1"), 1, "2026-11-10 08:00:00", "failed", nil, 2)
	a.clip(t, pool, s("E2"), 2, "2026-11-10 08:00:00", "deleted", nil, 3)
	a.clip(t, pool, s("E3"), 1, "2026-11-10 08:00:00", "pending", nil, 4)
	got, err := st.states(ctx, a.tenant, a.device, []string{"E1", "E2", "E3"})
	if err != nil || got[clipKey{"E1", 1}] != (clipState{failures: 2}) ||
		got[clipKey{"E2", 2}] != (clipState{live: true, deleted: true}) || got[clipKey{"E3", 1}] != (clipState{live: true}) {
		t.Fatalf("states %+v %v", got, err)
	}
	if n, err := st.pending(ctx, a.tenant); err != nil || n != 1 {
		t.Fatalf("pending %d %v", n, err)
	}

	cam := cams[0]
	ev := alarm(1, "E4", 147) // no video files
	at := time.Date(2026, 11, 10, 3, 0, 0, 0, time.UTC)
	if err := st.insertNoClip(ctx, a.tenant, cam, ev, cameraClock(at)); err != nil {
		t.Fatal(err)
	}
	if err := st.insertNoClip(ctx, a.tenant, cam, ev, cameraClock(at)); err != nil {
		t.Fatalf("second no-clip row should be ignored, got %v", err)
	}
	if got, _ := st.states(ctx, a.tenant, a.device, []string{"E4"}); got[clipKey{"E4", 0}] != (clipState{live: true}) {
		t.Fatalf("no clip state %+v", got)
	}
	if err := st.deleteNoClip(ctx, a.tenant, a.device, "E4"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.states(ctx, a.tenant, a.device, []string{"E4"}); len(got) != 0 {
		t.Fatalf("no clip not removed: %+v", got)
	}

	lock := pgSessionLock(pool)
	release, ok := lock(ctx)
	if !ok {
		t.Fatal("first lock refused")
	}
	if _, ok := lock(ctx); ok {
		t.Fatal("second instance got the lock while it was held")
	}
	release()
	release2, ok := lock(ctx)
	if !ok {
		t.Fatal("lock not released")
	}
	release2()
}
