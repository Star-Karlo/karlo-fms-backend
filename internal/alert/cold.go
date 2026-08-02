package alert

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// coldAlert maps an archived alert row (row_to_json of the alerts table) back to
// the fields the DTO needs. license_plate / driver_name are NOT in the archive
// (they were joins at query time) and are re-attached from the hot master data.
type coldAlert struct {
	ID           int64      `json:"id"`
	VehicleID    *int64     `json:"vehicle_id"`
	DriverID     *int64     `json:"driver_id"`
	Source       *string    `json:"source"`
	DeviceSource *string    `json:"device_source"`
	AlertCode    *string    `json:"alert_code"`
	AlertName    *string    `json:"alert_name"`
	Severity     *string    `json:"severity"`
	OccurredAt   *time.Time `json:"occurred_at"`
	ActualValue  *float64   `json:"actual_value"`
	LimitValue   *float64   `json:"limit_value"`
	Lat          *float64   `json:"lat"`
	Lon          *float64   `json:"lon"`
	MediaURL     *string    `json:"media_url"`
	Address      *string    `json:"address"`
	CheckStatus  string     `json:"check_status"`
	Note         *string    `json:"note"`
	CheckedAt    *time.Time `json:"checked_at"`
}

func (c *coldAlert) toDTO() DTO {
	return DTO{
		ID: c.ID, VehicleID: c.VehicleID, DriverID: c.DriverID,
		Source: c.Source, DeviceSource: c.DeviceSource,
		AlertCode: c.AlertCode, AlertName: c.AlertName, NameEN: nameEN(c.AlertCode), Severity: c.Severity,
		OccurredAt: c.OccurredAt, ActualValue: c.ActualValue, LimitValue: c.LimitValue,
		Lat: c.Lat, Lon: c.Lon, MediaURL: c.MediaURL, Address: c.Address,
		CheckStatus: c.CheckStatus, Note: c.Note, CheckedAt: c.CheckedAt,
	}
}

// alertFilter mirrors the SQL WHERE of ListAlerts, for filtering cold rows in Go.
type alertFilter struct {
	q, checkStatus, deviceSource, severity, alertCode string
	vehicleID                                         *int64
}

// matchNoText applies every filter except q (which needs the joined plate).
func (f alertFilter) matchNoText(c *coldAlert) bool {
	if f.checkStatus != "" && c.CheckStatus != f.checkStatus {
		return false
	}
	if f.deviceSource != "" && (c.DeviceSource == nil || *c.DeviceSource != f.deviceSource) {
		return false
	}
	if f.severity != "" && (c.Severity == nil || *c.Severity != f.severity) {
		return false
	}
	if f.alertCode != "" && (c.AlertCode == nil || *c.AlertCode != f.alertCode) {
		return false
	}
	if f.vehicleID != nil && (c.VehicleID == nil || *c.VehicleID != *f.vehicleID) {
		return false
	}
	return true
}

// boundary is now − alertDays: the hot/cold split. Rows older than this live in
// cold storage. Zero alertDays means no cold tier (hybrid disabled).
func (h *Handler) boundary() time.Time {
	return time.Now().Add(-time.Duration(h.alertDays) * 24 * time.Hour)
}

// coldEnabled reports whether a query with lower bound `from` should reach into
// cold storage: only when cold is configured and the caller explicitly asks for
// data older than the boundary. A missing/recent `from` stays hot-only (fast).
func (h *Handler) coldEnabled(from pgtype.Timestamptz) bool {
	return h.cold != nil && h.alertDays > 0 && from.Valid && from.Time.Before(h.boundary())
}

// coldAlerts loads archived alerts in [from, boundary) matching the filter, with
// plate/driver re-attached, sorted newest-first — the older half of a merged
// list. `to` further caps the upper bound when the caller set one below the
// boundary.
func (h *Handler) coldAlerts(ctx context.Context, tid int64, from time.Time, to pgtype.Timestamptz, f alertFilter) ([]DTO, error) {
	boundary := h.boundary()
	lines, err := h.cold.Lines(ctx, "alerts", tid, from, boundary)
	if err != nil {
		return nil, err
	}

	kept := make([]*coldAlert, 0, len(lines))
	for _, ln := range lines {
		var c coldAlert
		if err := json.Unmarshal(ln, &c); err != nil {
			continue // a malformed line never breaks the whole page
		}
		if c.OccurredAt == nil || !c.OccurredAt.Before(boundary) || c.OccurredAt.Before(from) {
			continue // cold owns strictly-older-than-boundary; respect the from bound
		}
		if to.Valid && c.OccurredAt.After(to.Time) {
			continue
		}
		if !f.matchNoText(&c) {
			continue
		}
		kept = append(kept, &c)
	}

	plates, drivers, err := h.enrich(ctx, tid, kept)
	if err != nil {
		return nil, err
	}

	q := strings.ToLower(f.q)
	out := make([]DTO, 0, len(kept))
	for _, c := range kept {
		d := c.toDTO()
		if c.VehicleID != nil {
			if p, ok := plates[*c.VehicleID]; ok {
				d.LicensePlate = &p
			}
		}
		if c.DriverID != nil {
			if n, ok := drivers[*c.DriverID]; ok {
				d.DriverName = &n
			}
		}
		if q != "" && !matchQ(q, d) {
			continue
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		return occ(out[i].OccurredAt).After(occ(out[j].OccurredAt))
	})
	return out, nil
}

// matchQ mirrors the SQL q filter: alert_name ILIKE OR license_plate ILIKE.
func matchQ(qLower string, d DTO) bool {
	if d.AlertName != nil && strings.Contains(strings.ToLower(*d.AlertName), qLower) {
		return true
	}
	if d.LicensePlate != nil && strings.Contains(strings.ToLower(*d.LicensePlate), qLower) {
		return true
	}
	return false
}

func occ(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// enrich resolves plate and driver-name for the cold rows from the hot master
// data (tenant-scoped), in two set queries rather than per-row.
func (h *Handler) enrich(ctx context.Context, tid int64, rows []*coldAlert) (plates, drivers map[int64]string, err error) {
	plates, drivers = map[int64]string{}, map[int64]string{}
	vids, dids := map[int64]struct{}{}, map[int64]struct{}{}
	for _, c := range rows {
		if c.VehicleID != nil {
			vids[*c.VehicleID] = struct{}{}
		}
		if c.DriverID != nil {
			dids[*c.DriverID] = struct{}{}
		}
	}
	if len(vids) == 0 && len(dids) == 0 {
		return plates, drivers, nil
	}
	err = db.Tx(ctx, h.pool, tid, func(tx pgx.Tx) error {
		if len(vids) > 0 {
			rows, err := tx.Query(ctx, `SELECT id, license_plate FROM vehicles WHERE id = ANY($1)`, keys(vids))
			if err != nil {
				return err
			}
			for rows.Next() {
				var id int64
				var plate *string
				if err := rows.Scan(&id, &plate); err != nil {
					rows.Close()
					return err
				}
				if plate != nil {
					plates[id] = *plate
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		if len(dids) > 0 {
			rows, err := tx.Query(ctx, `SELECT id, full_name FROM employees WHERE id = ANY($1)`, keys(dids))
			if err != nil {
				return err
			}
			for rows.Next() {
				var id int64
				var name string
				if err := rows.Scan(&id, &name); err != nil {
					rows.Close()
					return err
				}
				drivers[id] = name
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	return plates, drivers, err
}

func keys(m map[int64]struct{}) []int64 {
	out := make([]int64, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// coldAlertByID finds one archived alert by id within a single day's partition
// (the caller passes the occurred_at day as a hint, so this reads one file group
// rather than scanning the whole archive). Returns false when not found.
func (h *Handler) coldAlertByID(ctx context.Context, tid, id int64, day time.Time) (DTO, bool, error) {
	lines, err := h.cold.Lines(ctx, "alerts", tid, day, day)
	if err != nil {
		return DTO{}, false, err
	}
	for _, ln := range lines {
		var c coldAlert
		if err := json.Unmarshal(ln, &c); err != nil {
			continue
		}
		if c.ID != id {
			continue
		}
		dtos, err := h.decorate(ctx, tid, []*coldAlert{&c})
		if err != nil {
			return DTO{}, false, err
		}
		return dtos[0], true, nil
	}
	return DTO{}, false, nil
}

// decorate builds DTOs with plate/driver attached (shared by the by-id path).
func (h *Handler) decorate(ctx context.Context, tid int64, rows []*coldAlert) ([]DTO, error) {
	plates, drivers, err := h.enrich(ctx, tid, rows)
	if err != nil {
		return nil, err
	}
	out := make([]DTO, 0, len(rows))
	for _, c := range rows {
		d := c.toDTO()
		if c.VehicleID != nil {
			if p, ok := plates[*c.VehicleID]; ok {
				d.LicensePlate = &p
			}
		}
		if c.DriverID != nil {
			if n, ok := drivers[*c.DriverID]; ok {
				d.DriverName = &n
			}
		}
		out = append(out, d)
	}
	return out, nil
}
