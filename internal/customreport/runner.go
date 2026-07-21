package customreport

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/mailer"
	"github.com/karlo/fms-backend/internal/platform/db"
)

// The scheduled-report runner.
//
// Two properties this has to get right, both of which fail silently otherwise:
//
// 1. NO DOUBLE SENDS. next_run_at is advanced in the SAME transaction that
//    claims the row, with FOR UPDATE SKIP LOCKED. Two API instances polling
//    together will not both send: the second finds nothing due. Getting this
//    wrong means recipients get two copies and nobody reports it as a bug.
//
// 2. FAILURE IS VISIBLE. A schedule that stops sending is worse than one that
//    was never created, because the absence of an email is not something anyone
//    notices. Every outcome is written back to the row, and a failure still
//    advances next_run_at so one bad period does not wedge the schedule
//    forever — it retries next cycle and the error stays on the row.

// tickInterval is how often due schedules are looked for. Schedules fire on the
// hour, so a minute of granularity is far finer than needed and the query is a
// partial-index lookup on an empty result almost every time.
const tickInterval = time.Minute

// runTimeout bounds one report. Rendering can involve several telemetry
// round-trips plus a narration call; past this the schedule is better recorded
// as failed than left holding a transaction.
const runTimeout = 5 * time.Minute

// WithMailer attaches the email transport. Without one the runner does not
// start, and saved schedules simply sit there — better than a runner that
// renders reports every morning and drops them.
func (s *Service) WithMailer(m mailer.Sender) *Service {
	s.mail = m
	return s
}

// RunSchedules polls for due schedules until the context ends.
func (s *Service) RunSchedules(ctx context.Context) {
	if s.mail == nil {
		log.Printf("customreport: no mail transport configured; scheduled reports are disabled")
		return
	}
	log.Printf("customreport: schedule runner started (from %s)", s.mail.From())

	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.runDue(ctx)
		}
	}
}

func (s *Service) runDue(ctx context.Context) {
	// SystemTx ONLY to enumerate tenants — the house rule. Everything that
	// touches a schedule runs inside a tenant-scoped transaction, so RLS still
	// applies to the rows themselves.
	var tenants []int64
	if err := db.SystemTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM tenants`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			tenants = append(tenants, id)
		}
		return rows.Err()
	}); err != nil {
		log.Printf("customreport: list tenants: %v", err)
		return
	}

	for _, tenantID := range tenants {
		for {
			claimed, err := s.claimNext(ctx, tenantID)
			if err != nil {
				log.Printf("customreport: claim (tenant %d): %v", tenantID, err)
				break
			}
			if claimed == nil {
				break // nothing else due for this tenant
			}
			s.deliver(ctx, tenantID, *claimed)
		}
	}
}

// claimNext takes one due schedule and advances its next_run_at atomically.
func (s *Service) claimNext(ctx context.Context, tenantID int64) (*Schedule, error) {
	var out *Schedule
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		sc, err := scanSchedule(tx.QueryRow(ctx, `
SELECT `+scheduleCols+`, d.name
FROM report_schedules s
JOIN report_definitions d ON d.id = s.report_id
WHERE s.enabled AND s.next_run_at <= now()
ORDER BY s.next_run_at
FOR UPDATE OF s SKIP LOCKED
LIMIT 1`), true)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil
			}
			return err
		}

		// Advance BEFORE sending. A crash mid-send then costs one missed
		// report; advancing after would risk an infinite resend loop, which is
		// the worse failure — it reaches people.
		next := sc.NextRun(time.Now(), sc.location())
		if _, err := tx.Exec(ctx,
			`UPDATE report_schedules SET next_run_at=$1, last_run_at=now() WHERE id=$2`,
			next, sc.ID); err != nil {
			return err
		}
		out = sc
		return nil
	})
	return out, err
}

// deliver renders and emails one schedule, recording the outcome either way.
func (s *Service) deliver(ctx context.Context, tenantID int64, sc Schedule) {
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()

	err := s.sendOne(ctx, tenantID, sc)
	status, msg := "ok", ""
	if err != nil {
		status, msg = "error", err.Error()
		log.Printf("customreport: schedule %d (%s): %v", sc.ID, sc.ReportName, err)
	}
	// Recorded outside the claim transaction so a send that outlives it still
	// reports its outcome.
	if err := db.Tx(context.WithoutCancel(ctx), s.pool, tenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE report_schedules SET last_status=$1, last_error=$2 WHERE id=$3`,
			status, truncate(msg, 500), sc.ID)
		return e
	}); err != nil {
		log.Printf("customreport: recording outcome for schedule %d: %v", sc.ID, err)
	}
}

// SendScheduleNow sends a schedule immediately, without touching next_run_at.
// The outcome is still recorded, so a failed test shows up in the same place a
// failed scheduled run does.
func (s *Service) SendScheduleNow(ctx context.Context, tenantID, userID, id int64) error {
	if s.mail == nil {
		return fmt.Errorf("email is not configured on this deployment")
	}
	var sc *Schedule
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		var e error
		sc, e = scanSchedule(tx.QueryRow(ctx, `
SELECT `+scheduleCols+`, d.name
FROM report_schedules s
JOIN report_definitions d ON d.id = s.report_id
WHERE s.id = $1 AND s.user_id = $2`, id, userID), true)
		return e
	})
	if err != nil {
		return fmt.Errorf("schedule not found")
	}
	sendErr := s.sendOne(ctx, tenantID, *sc)
	status, msg := "ok", ""
	if sendErr != nil {
		status, msg = "error", sendErr.Error()
	}
	_ = db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE report_schedules SET last_run_at=now(), last_status=$1, last_error=$2 WHERE id=$3`,
			status, truncate(msg, 500), id)
		return e
	})
	return sendErr
}

func (s *Service) sendOne(ctx context.Context, tenantID int64, sc Schedule) error {
	var raw []byte
	var spec Spec
	err := db.Tx(ctx, s.pool, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT spec FROM report_definitions WHERE id=$1`, sc.ReportID).Scan(&raw)
	})
	if err != nil {
		return fmt.Errorf("report %d not found: %w", sc.ReportID, err)
	}
	if err := jsonUnmarshal(raw, &spec); err != nil {
		return fmt.Errorf("report %d has an unreadable spec: %w", sc.ReportID, err)
	}

	// The SCHEDULE decides the period, overriding whatever the report was saved
	// with — otherwise a weekly email sends the same frozen week forever.
	spec.Range = sc.Period(time.Now(), sc.location())
	spec.Format = sc.Format

	doc, err := s.Build(ctx, tenantID, spec)
	if err != nil {
		return fmt.Errorf("building the report: %w", err)
	}
	s.narrate(ctx, &doc, spec, sc.Format)

	out, err := Render(doc, sc.Format)
	if err != nil {
		return fmt.Errorf("rendering %s: %w", sc.Format, err)
	}

	subject := sc.Subject
	if strings.TrimSpace(subject) == "" {
		subject = fmt.Sprintf("%s — %s", spec.Name, doc.RangeLabel)
	}

	return s.mail.Send(ctx, mailer.Message{
		To:      sc.Recipients,
		Subject: subject,
		Body:    emailBody(spec, doc),
		Attachments: []mailer.Attachment{{
			Filename:    out.Filename,
			ContentType: out.ContentType,
			Body:        out.Body,
		}},
	})
}

// emailBody is plain text on purpose.
//
// The report IS the attachment; the body exists so the message is not a bare
// file with no context. It repeats the summary when there is one, because a
// recipient reading on a phone should be able to tell whether the attachment
// needs opening now or later.
func emailBody(spec Spec, doc Doc) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\r\n%s\r\n\r\n", spec.Name, doc.RangeLabel)
	if doc.Summary != "" {
		fmt.Fprintf(&b, "%s\r\n\r\n", doc.Summary)
	} else if spec.Notes != "" {
		fmt.Fprintf(&b, "%s\r\n\r\n", spec.Notes)
	}
	fmt.Fprintf(&b, "The full report is attached.\r\n\r\n")
	fmt.Fprintf(&b, "Sections:\r\n")
	for _, sec := range doc.Sections {
		rows := len(sec.Rows)
		switch rows {
		case 0:
			fmt.Fprintf(&b, "  - %s: no data for this period\r\n", sec.Title)
		case 1:
			fmt.Fprintf(&b, "  - %s: 1 row\r\n", sec.Title)
		default:
			fmt.Fprintf(&b, "  - %s: %d rows\r\n", sec.Title, rows)
		}
	}
	b.WriteString("\r\nSent automatically by Karlo FMS.\r\n")
	return b.String()
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
