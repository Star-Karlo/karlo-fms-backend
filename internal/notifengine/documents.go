package notifengine

// Document-expiry reminders (feedback epic 1.2).
//
// STNK, insurance, KIR and driver SIM expiries used to be visible only on the
// License page, for whoever remembered to open it. Once a day the engine now
// raises one DOCUMENT_EXPIRING alert per document inside the lead window —
// 14 days by default, or the threshold of an active DOCUMENT_EXPIRING rule
// when the company set its own — and again once it has expired. Filed against
// the vehicle (a driver's document goes on the truck they drive), so it lands
// where the fleet manager already looks and can be checked off like any alert.

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/alert"
	"github.com/karlo/fms-backend/internal/platform/conv"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/store"
)

const (
	documentAlertCode   = "DOCUMENT_EXPIRING"
	defaultDocumentLead = 14 // days, per the fleet managers' review
	documentRepeatDays  = 7  // remind again weekly until it is handled or renewed
)

// evalDocuments runs at most once per day per tenant (documents change daily
// at most; alerts repeat weekly anyway).
func (e *Engine) evalDocuments(ctx context.Context, tenantID int64) (int, error) {
	if last, ok := e.docLast[tenantID]; ok && time.Since(last) < 24*time.Hour {
		return 0, nil
	}
	e.docLast[tenantID] = time.Now()

	lead := int32(defaultDocumentLead)
	created := 0
	err := db.Tx(ctx, e.pool, tenantID, func(tx pgx.Tx) error {
		q := store.New(tx)
		// The company's own lead time, when it set one as a rule.
		if rules, err := q.ListRulesByCode(ctx, store.ListRulesByCodeParams{TenantID: tenantID, AlertCode: documentAlertCode}); err == nil {
			for _, r := range rules {
				if r.Status != "active" {
					return nil // explicitly switched off
				}
				if f := conv.NumF(r.Threshold); f != nil && *f > 0 {
					lead = int32(*f)
				}
			}
		}
		docs, err := q.ListExpiringDocuments(ctx, store.ListExpiringDocumentsParams{TenantID: tenantID, WithinDays: lead})
		if err != nil {
			return err
		}
		for _, d := range docs {
			code := documentAlertCode
			docID := float64(d.ID)
			n, err := q.CountRecentDocumentAlert(ctx, store.CountRecentDocumentAlertParams{
				TenantID: tenantID, AlertCode: &code, DocumentID: conv.Num(&docID), CooldownDays: documentRepeatDays,
			})
			if err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			kind := "Document"
			if d.Kind != nil && *d.Kind != "" {
				kind = *d.Kind
			} else if d.Title != nil && *d.Title != "" {
				kind = *d.Title
			}
			who := d.LicensePlate
			if d.DriverName != nil {
				who = *d.DriverName
				if d.LicensePlate != "" {
					who += " (" + d.LicensePlate + ")"
				}
			}
			var vid *int64
			if d.VehicleID != 0 {
				v := d.VehicleID
				vid = &v
			}
			exp := d.Expiry.Time.Format("02 Jan 2006")
			var name string
			sev := alert.SevImportant
			switch {
			case d.DaysLeft < 0:
				name = fmt.Sprintf("%s %s expired %d days ago (%s)", kind, who, -d.DaysLeft, exp)
				sev = alert.SevUrgent
			case d.DaysLeft == 0:
				name = fmt.Sprintf("%s %s expires today (%s)", kind, who, exp)
				sev = alert.SevUrgent
			default:
				name = fmt.Sprintf("%s %s expires in %d days (%s)", kind, who, d.DaysLeft, exp)
			}
			daysLeft := float64(d.DaysLeft)
			src := alert.SourceSystem
			if _, err := q.CreateAlert(ctx, store.CreateAlertParams{
				TenantID: tenantID, VehicleID: vid, AlertCode: &code, AlertName: &name, Severity: &sev,
				DeviceSource: &src, ActualValue: conv.Num(&daysLeft), LimitValue: conv.Num(&docID),
			}); err != nil {
				return err
			}
			created++
		}
		return nil
	})
	if created > 0 {
		log.Printf("notifengine: tenant %d: %d document-expiry alert(s) (lead %d days)", tenantID, created, lead)
	}
	return created, err
}
