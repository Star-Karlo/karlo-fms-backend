//go:build integration

package dashcam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/dbtest"
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

	h := &Handler{pool: pool}
	w := request(t, h, a.tenant, http.MethodGet, "/v1/video-clips/summary?from=2026-11-10&to=2026-11-11")
	if w.Code != http.StatusOK {
		t.Fatalf("summary: %d %s", w.Code, w.Body.String())
	}
	var got ClipSummaryDTO
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := ClipTotalsDTO{Events: 5, Clips: 3, Bytes: 5_500_000, Failed: 1, NoClip: 1, Pending: 1, Trucks: 1,
		AvgEventsPerTruckDay: 2.5, AvgBytesPerTruckDay: 2_750_000, AvgBytesPerClip: 5_500_000.0 / 3}
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
	if len(got.Trucks) != 1 || *got.Trucks[0].VehicleID != a.vehicle || got.Trucks[0].Bytes != 5_500_000 ||
		got.Trucks[0].LastEvent != "2026-11-11T23:30:00+00:00" {
		t.Fatalf("trucks %+v", got.Trucks)
	}

	// B sees only its own clip.
	w = request(t, h, b.tenant, http.MethodGet, "/v1/video-clips/summary?from=2026-11-10&to=2026-11-11")
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.Totals.Clips != 1 || got.Totals.Bytes != 7_000_000 {
		t.Fatalf("tenant B totals %+v", got.Totals)
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
