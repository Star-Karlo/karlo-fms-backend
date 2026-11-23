package dashcam

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// fakeTel serves stored events per camera and records every download started.
type fakeTel struct {
	alarms  map[string][]telemetry.DashcamAlarm
	started []telemetry.DashcamDownloadReq
	calls   int
	fail    error
}

func (f *fakeTel) DashcamCameraAlarms(_ context.Context, deviceID string, _, _ time.Time, since int64, limit int) ([]telemetry.DashcamAlarm, error) {
	f.calls++
	if f.fail != nil {
		return nil, f.fail
	}
	var out []telemetry.DashcamAlarm
	for _, a := range f.alarms[deviceID] {
		if a.ID > since && len(out) < limit {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeTel) StartDashcamDownload(_ context.Context, _ string, req telemetry.DashcamDownloadReq) (string, error) {
	f.started = append(f.started, req)
	return fmt.Sprintf("job-%d", len(f.started)), nil
}

// fakeStore keeps rows in memory, keyed like the unique index.
type fakeStore struct {
	cams     []autosaveCamera
	rows     map[clipKey][]string // statuses, oldest first
	pendingN int
	inserted []pendingClip
	noClips  []string
	removed  []string
}

func newFakeStore(cams ...autosaveCamera) *fakeStore {
	return &fakeStore{cams: cams, rows: map[clipKey][]string{}}
}

func (s *fakeStore) cameras(context.Context, int64) ([]autosaveCamera, error) { return s.cams, nil }

func (s *fakeStore) states(_ context.Context, _ int64, _ string, ids []string) (map[clipKey]clipState, error) {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	out := map[clipKey]clipState{}
	for k, statuses := range s.rows {
		if !want[k.alarmID] {
			continue
		}
		st := clipState{}
		for _, status := range statuses {
			switch status {
			case "failed":
				st.failures++
			case "deleted":
				st.live, st.deleted = true, true
			default:
				st.live = true
			}
		}
		out[k] = st
	}
	return out, nil
}

func (s *fakeStore) pending(context.Context, int64) (int, error) { return s.pendingN, nil }

func (s *fakeStore) insertPending(_ context.Context, pc pendingClip, _ string) (bool, error) {
	k := clipKey{*pc.alarmID, pc.channel}
	for _, st := range s.rows[k] {
		if st != "failed" {
			return false, nil // the unique index
		}
	}
	s.rows[k] = append(s.rows[k], "pending")
	s.pendingN++
	s.inserted = append(s.inserted, pc)
	return true, nil
}

func (s *fakeStore) insertNoClip(_ context.Context, _ int64, _ autosaveCamera, a telemetry.DashcamAlarm, _ time.Time) error {
	s.rows[clipKey{a.AlarmID, 0}] = append(s.rows[clipKey{a.AlarmID, 0}], "no_clip")
	s.noClips = append(s.noClips, a.AlarmID)
	return nil
}

func (s *fakeStore) deleteNoClip(_ context.Context, _ int64, _, alarmID string) error {
	k := clipKey{alarmID, 0}
	var kept []string
	for _, st := range s.rows[k] {
		if st != "no_clip" {
			kept = append(kept, st)
		}
	}
	s.rows[k] = kept
	s.removed = append(s.removed, alarmID)
	return nil
}

func alarm(id int64, alarmID string, code int, channels ...int) telemetry.DashcamAlarm {
	a := telemetry.DashcamAlarm{ID: id, DeviceID: "CAM1", AlarmID: alarmID, Code: code,
		OccurredAt: "2026-11-10T01:00:00+00:00", Files: []telemetry.DashcamAlarmFile{}}
	for _, ch := range channels {
		a.Files = append(a.Files, telemetry.DashcamAlarmFile{Channel: ch, FileType: 2,
			Start: "2026-11-10 07:59:55", Stop: "2026-11-10 08:00:05"})
	}
	return a
}

func newTestAutosaver(tel *fakeTel, st *fakeStore, maxInflight int) (*Autosaver, *int) {
	sleeps := 0
	a := &Autosaver{
		cfg: config.DashcamAutosaveConfig{Tenants: []int64{1}, Buckets: map[int64]string{1: "siba-bucket"},
			Interval: 30 * time.Minute, Lookback: 72 * time.Hour, MaxInflight: maxInflight},
		tel: tel, store: st,
		lock: func(context.Context) (func(), bool) { return func() {}, true },
		// Each wait stands in for the reconciler finishing every running download.
		sleep: func(context.Context, time.Duration) bool { sleeps++; st.pendingN = 0; return true },
		now:   func() time.Time { return time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC) },
	}
	return a, &sleeps
}

var cam1 = autosaveCamera{id: 7, deviceID: "CAM1", vehicleID: ptr(int64(70))}

func ptr[T any](v T) *T { return &v }

func TestAutosaveStartsEveryCameraOfEveryEvent(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {alarm(1, "E1", 124, 1, 2)}}}
	st := newFakeStore(cam1)
	a, _ := newTestAutosaver(tel, st, 20)

	s := a.sweepTenant(context.Background(), 1)

	if s.events != 1 || s.started != 2 || len(tel.started) != 2 {
		t.Fatalf("stats %+v, started %+v", s, tel.started)
	}
	for i, req := range tel.started {
		if req.AlarmID != "E1" || req.Bucket != "siba-bucket" || req.Channel != i+1 {
			t.Fatalf("download %d: %+v", i, req)
		}
	}
	pc := st.inserted[0]
	// 01:00 UTC is 08:00 WIB, stored as camera clock time like manual saves.
	if pc.reason != "auto:124" || pc.start != "2026-11-10 08:00:00" || pc.stop != "2026-11-10 08:00:05" || *pc.vehicleID != 70 {
		t.Fatalf("pending clip %+v", pc)
	}
}

func TestAutosaveSkipsSavedDeletedAndFailedThreeTimes(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {
		alarm(1, "SAVED", 124, 1), alarm(2, "PENDING", 124, 1), alarm(3, "DELETED", 124, 1),
		alarm(4, "FAILED3", 124, 1), alarm(5, "FAILED2", 124, 1),
	}}}
	st := newFakeStore(cam1)
	st.rows[clipKey{"SAVED", 1}] = []string{"ready"}
	st.rows[clipKey{"PENDING", 1}] = []string{"pending"}
	st.rows[clipKey{"DELETED", 1}] = []string{"deleted"}
	st.rows[clipKey{"FAILED3", 1}] = []string{"failed", "failed", "failed"}
	st.rows[clipKey{"FAILED2", 1}] = []string{"failed", "failed"}
	a, _ := newTestAutosaver(tel, st, 20)

	s := a.sweepTenant(context.Background(), 1)

	if len(tel.started) != 1 || tel.started[0].AlarmID != "FAILED2" {
		t.Fatalf("started %+v, want only the third try of FAILED2", tel.started)
	}
	if s.skipped != 3 || s.deleted != 1 || s.started != 1 {
		t.Fatalf("stats %+v", s)
	}
}

func TestAutosaveDrainsBacklogPastTheLimitInOneRun(t *testing.T) {
	var evs []telemetry.DashcamAlarm
	for i := 1; i <= 7; i++ {
		evs = append(evs, alarm(int64(i), fmt.Sprintf("E%d", i), 124, 1))
	}
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": evs}}
	st := newFakeStore(cam1)
	a, sleeps := newTestAutosaver(tel, st, 2)

	s := a.sweepTenant(context.Background(), 1)

	if s.started != 7 || len(tel.started) != 7 {
		t.Fatalf("started %d of 7: %+v", s.started, s)
	}
	if *sleeps != 3 || s.waits != 3 {
		t.Fatalf("waits %d (sleeps %d), want 3 (after 2, 4 and 6 downloads)", s.waits, *sleeps)
	}
}

func TestAutosaveStopsWaitingWhenShuttingDown(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {alarm(1, "E1", 124, 1), alarm(2, "E2", 124, 1)}}}
	st := newFakeStore(cam1)
	a, _ := newTestAutosaver(tel, st, 1)
	a.sleep = func(context.Context, time.Duration) bool { return false }

	a.sweepTenant(context.Background(), 1)

	if len(tel.started) != 1 {
		t.Fatalf("started %d, want 1 before the shutdown", len(tel.started))
	}
}

func TestAutosaveNoClipThenClip(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {alarm(1, "LATE", 122)}}}
	st := newFakeStore(cam1)
	a, _ := newTestAutosaver(tel, st, 20)

	a.sweepTenant(context.Background(), 1)
	a.sweepTenant(context.Background(), 1) // a second run doesn't add another "no clip" row
	if len(st.noClips) != 1 || len(tel.started) != 0 {
		t.Fatalf("no clips %v, started %v", st.noClips, tel.started)
	}

	tel.alarms["CAM1"] = []telemetry.DashcamAlarm{alarm(1, "LATE", 122, 2)} // the clip uploaded
	a.sweepTenant(context.Background(), 1)

	if len(tel.started) != 1 || tel.started[0].Channel != 2 {
		t.Fatalf("started %+v", tel.started)
	}
	if len(st.removed) != 1 || len(st.rows[clipKey{"LATE", 0}]) != 0 {
		t.Fatalf("no-clip row not removed: %v", st.rows[clipKey{"LATE", 0}])
	}
}

func TestAutosaveTenantNotEnabledMakesNoCalls(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {alarm(1, "E1", 124, 1)}}}
	st := newFakeStore(cam1)
	a, _ := newTestAutosaver(tel, st, 20)
	a.cfg.Tenants = nil
	ran := false
	a.lock = func(context.Context) (func(), bool) { ran = true; return func() {}, true }

	a.Run(context.Background()) // returns at once

	if ran || tel.calls != 0 || len(tel.started) != 0 {
		t.Fatalf("auto-save ran for no tenants: lock %v, calls %d", ran, tel.calls)
	}
}

func TestAutosaveSkipsTurnWhenLockHeld(t *testing.T) {
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": {alarm(1, "E1", 124, 1)}}}
	st := newFakeStore(cam1)
	a, _ := newTestAutosaver(tel, st, 20)
	a.lock = func(context.Context) (func(), bool) { return nil, false }

	a.runOnce(context.Background())

	if tel.calls != 0 || len(tel.started) != 0 {
		t.Fatalf("ran without the lock: calls %d", tel.calls)
	}
}

func TestAutosaveCameraErrorDoesNotStopOtherCameras(t *testing.T) {
	cam2 := autosaveCamera{id: 8, deviceID: "CAM2"}
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM2": {alarm(1, "E1", 124, 1)}}}
	st := newFakeStore(cam1, cam2)
	a, _ := newTestAutosaver(tel, st, 20)
	calls := 0
	a.tel = &erroringFirst{fakeTel: tel, calls: &calls}

	s := a.sweepTenant(context.Background(), 1)

	if s.cameraErrors != 1 || s.started != 1 {
		t.Fatalf("stats %+v", s)
	}
}

type erroringFirst struct {
	*fakeTel
	calls *int
}

func (e *erroringFirst) DashcamCameraAlarms(ctx context.Context, deviceID string, from, to time.Time, since int64, limit int) ([]telemetry.DashcamAlarm, error) {
	*e.calls++
	if *e.calls == 1 {
		return nil, errors.New("telemetry unreachable")
	}
	return e.fakeTel.DashcamCameraAlarms(ctx, deviceID, from, to, since, limit)
}

func TestAutosavePagesThroughEvents(t *testing.T) {
	var evs []telemetry.DashcamAlarm
	for i := 1; i <= alarmPage+5; i++ {
		evs = append(evs, alarm(int64(i), fmt.Sprintf("E%d", i), 124))
	}
	tel := &fakeTel{alarms: map[string][]telemetry.DashcamAlarm{"CAM1": evs}}
	a, _ := newTestAutosaver(tel, newFakeStore(cam1), 20)

	got, err := a.cameraAlarms(context.Background(), "CAM1", time.Time{}, time.Time{})

	if err != nil || len(got) != alarmPage+5 || tel.calls != 2 {
		t.Fatalf("got %d events in %d calls, err %v", len(got), tel.calls, err)
	}
}

func TestEventOfReadsAutoReasons(t *testing.T) {
	code, name := eventOf("auto:124")
	if code == nil || *code != 124 || name == nil || *name == "" {
		t.Fatalf("auto:124 -> %v %v", code, name)
	}
	if c, n := eventOf("manual"); c != nil || n != nil {
		t.Fatalf("manual -> %v %v", c, n)
	}
}

func TestTypeOfKeepsUnparsedReasons(t *testing.T) {
	if got := typeOf(store.AutoClipSummaryByTypeRow{Reason: "auto:x", Events: 2}); got.Code != nil || got.Name != "auto:x" || got.Events != 2 {
		t.Fatalf("unparsed reason -> %+v", got)
	}
	_, want, _ := alarmMeta(25)
	if got := typeOf(store.AutoClipSummaryByTypeRow{Reason: "auto:25"}); got.Code == nil || *got.Code != 25 || got.Name != want {
		t.Fatalf("auto:25 -> %+v", got)
	}
}

func TestDayWindowUsesWIBMidnightsLabelledUTC(t *testing.T) {
	from, to, err := dayWindow("2026-11-10", "2026-11-12")
	if err != nil || !from.Equal(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)) || !to.Equal(time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("window %v..%v err %v", from, to, err)
	}
	if _, _, err := dayWindow("10/11/2026", ""); err == nil {
		t.Fatal("bad date accepted")
	}
}
