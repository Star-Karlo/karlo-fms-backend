package dashcam

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/platform/config"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
	"github.com/karlo/fms-backend/internal/telemetry"
)

// Autosaver copies every camera event clip of the enabled customers to S3, so
// nobody has to click Save. It reads events from telemetry's stored copy (no
// camera-platform calls), asks telemetry to copy each event's clip by event id
// (never from the SD card), and records each clip as pending; ReconcileClips
// finishes them.
type Autosaver struct {
	cfg   config.DashcamAutosaveConfig
	tel   autosaveTelemetry
	store autosaveStore
	lock  func(ctx context.Context) (release func(), ok bool)
	sleep func(ctx context.Context, d time.Duration) bool
	now   func() time.Time
}

type autosaveTelemetry interface {
	DashcamCameraAlarms(ctx context.Context, deviceID string, from, to time.Time, since int64, limit int) ([]telemetry.DashcamAlarm, error)
	StartDashcamDownload(ctx context.Context, deviceID string, req telemetry.DashcamDownloadReq) (string, error)
}

type autosaveStore interface {
	cameras(ctx context.Context, tenantID int64) ([]autosaveCamera, error)
	states(ctx context.Context, tenantID int64, deviceID string, alarmIDs []string) (map[clipKey]clipState, error)
	pending(ctx context.Context, tenantID int64) (int, error)
	insertPending(ctx context.Context, pc pendingClip, jobID string) (bool, error)
	insertNoClip(ctx context.Context, tenantID int64, cam autosaveCamera, a telemetry.DashcamAlarm, start time.Time) error
	deleteNoClip(ctx context.Context, tenantID int64, deviceID, alarmID string) error
}

type autosaveCamera struct {
	id        int64
	deviceID  string
	vehicleID *int64
}

// clipKey is one event + camera channel; channel 0 is the "no clip" row.
type clipKey struct {
	alarmID string
	channel int
}

type clipState struct {
	live     bool // a row that isn't failed: pending, ready, no_clip or deleted
	deleted  bool // a user removed it; never save again
	failures int
}

type sweepStats struct {
	events, started, skipped, noClip, deleted, waits, cameraErrors int
}

const (
	maxAutosaveTries = 3
	alarmPage        = 1000
	// The reconciler finishes clips every 8 s, so a full download limit is
	// checked again a little after that.
	slotWait = 10 * time.Second
)

// wib is the fleet's time zone; cameras report WIB clock time.
var wib = time.FixedZone("WIB", 7*3600)

// NewAutosaver builds the auto-save. It does nothing unless cfg lists tenants.
func NewAutosaver(pool *db.Pool, tel *telemetry.Client, cfg config.DashcamAutosaveConfig) *Autosaver {
	if cfg.MaxInflight < 1 {
		cfg.MaxInflight = 1
	}
	return &Autosaver{
		cfg: cfg, tel: tel, store: pgAutosaveStore{pool},
		lock:  pgSessionLock(pool),
		sleep: sleepCtx,
		now:   time.Now,
	}
}

// Run sweeps about a minute after start, then every Interval. A run that
// overruns the interval delays the next one; runs never overlap.
func (a *Autosaver) Run(ctx context.Context) {
	if len(a.cfg.Tenants) == 0 {
		return
	}
	log.Printf("dashcam autosave: on for tenants %v, every %s, looking back %s", a.cfg.Tenants, a.cfg.Interval, a.cfg.Lookback)
	if !a.sleep(ctx, time.Minute) {
		return
	}
	t := time.NewTicker(a.cfg.Interval)
	defer t.Stop()
	for {
		a.runOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (a *Autosaver) runOnce(ctx context.Context) {
	release, ok := a.lock(ctx)
	if !ok {
		log.Printf("dashcam autosave: another instance holds the lock; skipping this turn")
		return
	}
	defer release()
	for _, tid := range a.cfg.Tenants {
		s := a.sweepTenant(ctx, tid)
		log.Printf("dashcam autosave: tenant %d: %d events, %d downloads started, %d skipped, %d no clip, %d deleted (skipped), %d waits at the limit, %d camera errors",
			tid, s.events, s.started, s.skipped, s.noClip, s.deleted, s.waits, s.cameraErrors)
	}
}

func (a *Autosaver) sweepTenant(ctx context.Context, tid int64) sweepStats {
	var s sweepStats
	cams, err := a.store.cameras(ctx, tid)
	if err != nil {
		log.Printf("dashcam autosave: tenant %d: list cameras: %v", tid, err)
		s.cameraErrors++
		return s
	}
	inflight, err := a.store.pending(ctx, tid)
	if err != nil {
		log.Printf("dashcam autosave: tenant %d: count downloads: %v", tid, err)
		s.cameraErrors++
		return s
	}
	to := a.now()
	from := to.Add(-a.cfg.Lookback)
	for _, cam := range cams {
		if ctx.Err() != nil {
			return s
		}
		alarms, err := a.cameraAlarms(ctx, cam.deviceID, from, to)
		if err != nil {
			// Telemetry unreachable: the next run covers this camera.
			log.Printf("dashcam autosave: tenant %d camera %s: events: %v", tid, cam.deviceID, err)
			s.cameraErrors++
			continue
		}
		if err := a.sweepCamera(ctx, tid, cam, alarms, &inflight, &s); err != nil {
			log.Printf("dashcam autosave: tenant %d camera %s: %v", tid, cam.deviceID, err)
			s.cameraErrors++
		}
	}
	return s
}

func (a *Autosaver) cameraAlarms(ctx context.Context, deviceID string, from, to time.Time) ([]telemetry.DashcamAlarm, error) {
	var all []telemetry.DashcamAlarm
	var since int64
	for {
		page, err := a.tel.DashcamCameraAlarms(ctx, deviceID, from, to, since, alarmPage)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < alarmPage {
			return all, nil
		}
		since = page[len(page)-1].ID
	}
}

func (a *Autosaver) sweepCamera(ctx context.Context, tid int64, cam autosaveCamera, alarms []telemetry.DashcamAlarm, inflight *int, s *sweepStats) error {
	ids := make([]string, 0, len(alarms))
	for _, al := range alarms {
		if al.AlarmID != "" {
			ids = append(ids, al.AlarmID)
		}
	}
	states, err := a.store.states(ctx, tid, cam.deviceID, ids)
	if err != nil {
		return fmt.Errorf("read saved clips: %w", err)
	}
	for _, al := range alarms {
		at, err := time.Parse(time.RFC3339, al.OccurredAt)
		if al.AlarmID == "" || err != nil {
			continue
		}
		s.events++
		start := cameraClock(at)
		noClip := states[clipKey{al.AlarmID, 0}]
		if len(al.Files) == 0 {
			if !noClip.live {
				if err := a.store.insertNoClip(ctx, tid, cam, al, start); err != nil {
					return fmt.Errorf("record no clip: %w", err)
				}
				s.noClip++
			}
			continue
		}
		for _, f := range al.Files {
			st := states[clipKey{al.AlarmID, f.Channel}]
			switch {
			case st.deleted:
				s.deleted++
				continue
			case st.live || st.failures >= maxAutosaveTries:
				s.skipped++
				continue
			}
			if !a.waitForSlot(ctx, tid, inflight, s) {
				return nil
			}
			startStr, stopStr := start.Format(clipStartLayout), clipStop(start, f.Stop)
			jobID, err := a.tel.StartDashcamDownload(ctx, cam.deviceID, telemetry.DashcamDownloadReq{
				Channel: f.Channel, Start: startStr, Stop: stopStr, AlarmID: al.AlarmID, Bucket: a.cfg.Buckets[tid],
			})
			if err != nil {
				return fmt.Errorf("start download: %w", err)
			}
			alarmID := al.AlarmID
			inserted, err := a.store.insertPending(ctx, pendingClip{
				tenantID: tid, dashcamID: cam.id, vehicleID: cam.vehicleID, deviceID: cam.deviceID,
				channel: f.Channel, start: startStr, stop: stopStr, lat: al.Lat, lon: al.Lon,
				reason: fmt.Sprintf("auto:%d", al.Code), alarmID: &alarmID,
			}, jobID)
			if err != nil {
				return fmt.Errorf("record download: %w", err)
			}
			if !inserted {
				s.skipped++
				continue
			}
			states[clipKey{al.AlarmID, f.Channel}] = clipState{live: true}
			*inflight++
			s.started++
		}
		if noClip.live && !noClip.deleted {
			if err := a.store.deleteNoClip(ctx, tid, cam.deviceID, al.AlarmID); err != nil {
				return fmt.Errorf("remove no clip: %w", err)
			}
		}
	}
	return nil
}

// waitForSlot blocks while the customer has MaxInflight downloads running, so
// a big backlog is drained within one run instead of waiting for the next.
// false when the backend is shutting down.
func (a *Autosaver) waitForSlot(ctx context.Context, tid int64, inflight *int, s *sweepStats) bool {
	for *inflight >= a.cfg.MaxInflight {
		s.waits++
		if !a.sleep(ctx, slotWait) {
			return false
		}
		if n, err := a.store.pending(ctx, tid); err == nil {
			*inflight = n
		}
	}
	return true
}

// cameraClock turns an instant into the camera's WIB clock time labelled UTC,
// which is how video_clips.clip_start stores every clip (manual saves too).
func cameraClock(t time.Time) time.Time {
	w := t.In(wib)
	return time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, time.UTC)
}

// clipStop is the file's own stop time, or 10 s after the start when the camera
// didn't report one.
func clipStop(start time.Time, fileStop string) string {
	if t, err := time.Parse(clipStartLayout, fileStop); err == nil && t.After(start) {
		return fileStop
	}
	return start.Add(10 * time.Second).Format(clipStartLayout)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// pgSessionLock takes the auto-save advisory lock on one held connection: a
// session lock lives on its connection, so it must be unlocked on that same
// connection, not on whichever one the pool hands out next.
func pgSessionLock(pool *db.Pool) func(ctx context.Context) (func(), bool) {
	return func(ctx context.Context) (func(), bool) {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			log.Printf("dashcam autosave: lock: %v", err)
			return nil, false
		}
		var ok bool
		if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1, 0)", int32(db.DashcamAutosaveLock)).Scan(&ok); err != nil || !ok {
			conn.Release()
			return nil, false
		}
		return func() {
			if _, err := conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1, 0)", int32(db.DashcamAutosaveLock)); err != nil {
				// Closing the connection drops the lock with it.
				_ = conn.Conn().Close(context.Background())
			}
			conn.Release()
		}, true
	}
}

// pgAutosaveStore is the auto-save's data access, every query tenant-scoped.
type pgAutosaveStore struct{ pool *db.Pool }

func (p pgAutosaveStore) cameras(ctx context.Context, tid int64) ([]autosaveCamera, error) {
	var out []autosaveCamera
	err := db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
		rows, err := store.New(tx).AutosaveCameras(ctx)
		for _, r := range rows {
			out = append(out, autosaveCamera{id: r.ID, deviceID: r.VssDeviceID, vehicleID: r.VehicleID})
		}
		return err
	})
	return out, err
}

func (p pgAutosaveStore) states(ctx context.Context, tid int64, deviceID string, alarmIDs []string) (map[clipKey]clipState, error) {
	out := map[clipKey]clipState{}
	err := db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
		rows, err := store.New(tx).AutoClipStates(ctx, store.AutoClipStatesParams{VssDeviceID: deviceID, AlarmIds: alarmIDs})
		for _, r := range rows {
			if r.AlarmID == nil {
				continue
			}
			k := clipKey{*r.AlarmID, int(r.Channel)}
			st := out[k]
			switch r.Status {
			case "failed":
				st.failures++
			case "deleted":
				st.live, st.deleted = true, true
			default:
				st.live = true
			}
			out[k] = st
		}
		return err
	})
	return out, err
}

func (p pgAutosaveStore) pending(ctx context.Context, tid int64) (int, error) {
	var n int64
	err := db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
		var err error
		n, err = store.New(tx).CountPendingAutoClips(ctx)
		return err
	})
	return int(n), err
}

func (p pgAutosaveStore) insertPending(ctx context.Context, pc pendingClip, jobID string) (bool, error) {
	_, err := insertPendingClip(ctx, p.pool, pc, jobID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // the event + camera already has a live row
	}
	return err == nil, err
}

func (p pgAutosaveStore) insertNoClip(ctx context.Context, tid int64, cam autosaveCamera, a telemetry.DashcamAlarm, start time.Time) error {
	alarmID := a.AlarmID
	return db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
		_, err := store.New(tx).InsertNoClip(ctx, store.InsertNoClipParams{
			TenantID: tid, VehicleID: cam.vehicleID, VssDeviceID: cam.deviceID,
			ClipStart: pgtype.Timestamptz{Time: start, Valid: true},
			Lat:       a.Lat, Lon: a.Lon, Reason: fmt.Sprintf("auto:%d", a.Code), AlarmID: &alarmID,
		})
		return err
	})
}

func (p pgAutosaveStore) deleteNoClip(ctx context.Context, tid int64, deviceID, alarmID string) error {
	return db.Tx(ctx, p.pool, tid, func(tx pgx.Tx) error {
		return store.New(tx).DeleteNoClip(ctx, store.DeleteNoClipParams{VssDeviceID: deviceID, AlarmID: &alarmID})
	})
}
