package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/platform/db"
)

// Dispatcher pushes new alerts to each tenant's enabled webhook endpoints,
// HMAC-signed, with exponential backoff on failure. It runs as a background
// ticker in the API process, like the notification engine.
//
// Two phases per tick, per tenant: DISCOVER (enqueue a delivery row for each new
// alert past the endpoint's cursor) then DELIVER (attempt every due delivery).
// HTTP happens outside the DB transaction so a slow receiver never holds a
// connection.
type Dispatcher struct {
	pool        *db.Pool
	client      *http.Client
	every       time.Duration
	maxAttempts int
}

func NewDispatcher(pool *db.Pool) *Dispatcher {
	return &Dispatcher{
		pool:        pool,
		client:      &http.Client{Timeout: 10 * time.Second},
		every:       15 * time.Second,
		maxAttempts: 6, // ~ up to a few hours of backoff before giving up
	}
}

func (d *Dispatcher) Run(ctx context.Context) {
	t := time.NewTicker(d.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.tick(ctx); err != nil {
				log.Printf("webhook dispatcher: %v", err)
			}
		}
	}
}

func (d *Dispatcher) tick(ctx context.Context) error {
	var tenants []int64
	rows, err := d.pool.Query(ctx, `SELECT id FROM tenants`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			tenants = append(tenants, id)
		}
	}
	rows.Close()

	for _, tid := range tenants {
		d.discover(ctx, tid)
		d.deliverDue(ctx, tid)
	}
	return nil
}

// discover enqueues a delivery for each new alert past every enabled endpoint's
// cursor, then advances the cursor. One INSERT..SELECT per endpoint.
func (d *Dispatcher) discover(ctx context.Context, tenantID int64) {
	_ = db.Tx(ctx, d.pool, tenantID, func(tx pgx.Tx) error {
		eps, err := tx.Query(ctx, `SELECT id, last_alert_id FROM webhook_endpoints WHERE enabled`)
		if err != nil {
			return err
		}
		type ep struct{ id, cursor int64 }
		var list []ep
		for eps.Next() {
			var e ep
			if err := eps.Scan(&e.id, &e.cursor); err == nil {
				list = append(list, e)
			}
		}
		eps.Close()
		for _, e := range list {
			if _, err := tx.Exec(ctx, `
INSERT INTO webhook_deliveries (tenant_id, endpoint_id, alert_id)
SELECT $1, $2, a.id FROM alerts a WHERE a.id > $3
ON CONFLICT (endpoint_id, alert_id) DO NOTHING`, tenantID, e.id, e.cursor); err != nil {
				log.Printf("webhook discover t%d ep%d: %v", tenantID, e.id, err)
				continue
			}
			_, _ = tx.Exec(ctx, `
UPDATE webhook_endpoints SET last_alert_id = COALESCE((SELECT max(id) FROM alerts),last_alert_id)
WHERE id = $1`, e.id)
		}
		return nil
	})
}

// due is one delivery ready to attempt, with everything the POST needs.
type due struct {
	deliveryID int64
	url        string
	secret     string
	attempts   int
	payload    map[string]any
}

// deliverDue attempts every delivery that is pending/failed and due. Reads the
// batch in a tx, POSTs outside it, then writes results back.
func (d *Dispatcher) deliverDue(ctx context.Context, tenantID int64) {
	var batch []due
	_ = db.Tx(ctx, d.pool, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
SELECT wd.id, e.url, e.secret, wd.attempts,
       a.id, a.alert_code, a.severity, a.source,
       to_char(a.occurred_at,'YYYY-MM-DD"T"HH24:MI:SSOF:00'),
       v.license_plate, emp.full_name, a.lat, a.lon, a.payload
FROM webhook_deliveries wd
JOIN webhook_endpoints e ON e.id = wd.endpoint_id AND e.enabled
JOIN alerts a ON a.id = wd.alert_id
LEFT JOIN vehicles v   ON v.id = a.vehicle_id
LEFT JOIN employees emp ON emp.id = v.current_driver_id
WHERE wd.status <> 'success' AND wd.attempts < $1 AND wd.next_attempt_at <= now()
ORDER BY wd.id LIMIT 100`, d.maxAttempts)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var x due
			var alertID int64
			var code, sev, src, occurred *string
			var plate, driver *string
			var lat, lon *float64
			var payload map[string]any
			if err := rows.Scan(&x.deliveryID, &x.url, &x.secret, &x.attempts,
				&alertID, &code, &sev, &src, &occurred, &plate, &driver, &lat, &lon, &payload); err != nil {
				return err
			}
			x.payload = map[string]any{
				"type": "alert", "alert_id": alertID,
				"code": strDeref(code), "severity": strDeref(sev), "source": strDeref(src),
				"occurred_at": strDeref(occurred), "license_plate": strDeref(plate),
				"driver": strDeref(driver), "lat": lat, "lon": lon, "detail": payload,
			}
			batch = append(batch, x)
		}
		return rows.Err()
	})

	for _, x := range batch {
		status, err := deliver(ctx, x.url, x.secret, x.payload)
		d.record(ctx, tenantID, x, status, err)
	}
}

// record writes the outcome: success, or a backed-off retry (2^attempts minutes,
// capped at 60), or terminal failure at maxAttempts.
func (d *Dispatcher) record(ctx context.Context, tenantID int64, x due, status int, derr error) {
	_ = db.Tx(ctx, d.pool, tenantID, func(tx pgx.Tx) error {
		if derr == nil {
			_, err := tx.Exec(ctx, `
UPDATE webhook_deliveries SET status='success', attempts=attempts+1, last_status=$2,
       last_error=NULL, updated_at=now() WHERE id=$1`, x.deliveryID, status)
			return err
		}
		backoff := time.Duration(1<<uint(x.attempts)) * time.Minute
		if backoff > time.Hour {
			backoff = time.Hour
		}
		next := time.Now().Add(backoff)
		st := "failed"
		if x.attempts+1 >= d.maxAttempts {
			st = "failed" // terminal, but attempts cap stops re-selection
		}
		_, err := tx.Exec(ctx, `
UPDATE webhook_deliveries SET status=$2, attempts=attempts+1, last_status=$3,
       last_error=$4, next_attempt_at=$5, updated_at=now() WHERE id=$1`,
			x.deliveryID, st, status, derr.Error(), next)
		return err
	})
}

// deliver POSTs the JSON body to url, signing it with an HMAC-SHA256 of the body
// under secret in X-FMS-Signature. Returns the HTTP status (0 on transport
// error) and an error for any non-2xx or transport failure.
func deliver(ctx context.Context, url, secret string, body map[string]any) (int, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Karlo-FMS-Webhook/1")
	if secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(buf)
		req.Header.Set("X-FMS-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain so the connection can be reused; a short read here changes
	// nothing about the delivery, which is judged on the status code below.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("non-2xx status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
