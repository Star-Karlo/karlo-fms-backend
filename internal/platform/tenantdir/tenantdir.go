// Package tenantdir answers "which tenants exist, and what are they called"
// from the shared authentication service.
//
// WHY THIS EXISTS. Fifteen places in this service ran `SELECT id FROM tenants`
// or `SELECT id, name FROM tenants` — eleven background workers iterating every
// tenant (webhook dispatch, retention, the dashcam pollers, telemetry sync,
// driver scoring, custom reports) and four building a dropdown. All of them
// were reading a table that is no longer the record of which customers exist.
//
// After the cutover a company is created in karlo_auth and allocated an
// fms_tenant_id there, so `fms_app.tenants` can be missing a tenant entirely.
// Row-level security still works — it only compares the bigint — but a worker
// iterating the local table would silently skip that customer: no webhooks, no
// retention, no driver scores, and nothing anywhere saying why.
//
// SERVICE CREDENTIAL, NOT A USER TOKEN. This runs in a background loop with no
// request behind it, so there is no caller's token to forward. An earlier
// version borrowed a user's token, which would have tied every customer's
// webhooks and retention sweeps to one person's account and stopped the day
// they left. The platform's service-to-service path is gRPC with a shared
// service token, which is what iamclient carries.
//
// CACHED, because these are hot paths. The list changes when somebody onboards
// a customer, which is rare, while the pollers ask on every tick. A short TTL
// keeps a new customer appearing within a minute without turning a background
// sweep into a request storm.
//
// STALE ON FAILURE, deliberately. If the auth service is unreachable the last
// good list is served rather than an error: the alternative is that an auth
// outage stops telemetry sync and webhook delivery for every customer, which
// is a far worse failure than a newly onboarded tenant waiting a few minutes.
// The staleness is bounded by the process lifetime and logged.
package tenantdir

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/karlo/fms-backend/internal/platform/iamclient"
)

// Tenant is one FMS customer: the bigint this service keys everything by, and
// the name to show a human.
type Tenant struct {
	ID   int64
	Name string
	// CompanyID is the shared IAM's UUID for this customer. Carried because it
	// is the only thing a caller from outside FMS knows them by: the acting-for
	// header names a COMPANY, and every other service on the platform speaks
	// UUIDs. Resolving it here means fms-backend still keys everything by the
	// bigint, and translates once, at the edge.
	CompanyID string
	// APIEnabled is the company-wide API grant, as karlo_auth holds it.
	//
	// Carried here because it is the only way FMS can honour a link that was
	// cut in Karlo admin for a key FMS issued ITSELF: such a key is unknown
	// upstream, so nothing on the validate path would ever consult the switch.
	// Cached with the rest of the directory and invalidated by the same
	// company notice, so flipping the switch takes effect here within the TTL
	// at worst and immediately in practice.
	APIEnabled bool
}

// Directory lists the tenants this service knows about.
type Directory struct {
	iam *iamclient.Client
	ttl time.Duration

	mu       sync.RWMutex
	cached   []Tenant
	cachedAt time.Time
}

// New returns nil when there is no client, which leaves callers on the local
// table — the state of a deployment that has not cut over.
func New(iam *iamclient.Client, ttl time.Duration) *Directory {
	if iam == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &Directory{iam: iam, ttl: ttl}
}

// List returns every company holding an FMS tenant id.
func (d *Directory) List(ctx context.Context) ([]Tenant, error) {
	d.mu.RLock()
	fresh := time.Since(d.cachedAt) < d.ttl && d.cached != nil
	cached := d.cached
	d.mu.RUnlock()
	if fresh {
		return cached, nil
	}

	got, err := d.fetch(ctx)
	if err != nil {
		if cached != nil {
			log.Printf("tenantdir: serving a %s-old list, refresh failed: %v",
				time.Since(d.cachedAt).Round(time.Second), err)
			return cached, nil
		}
		return nil, err
	}

	d.mu.Lock()
	d.cached, d.cachedAt = got, time.Now()
	d.mu.Unlock()
	return got, nil
}

// APIEnabledForTenant reports whether this tenant's company may be reached
// with an API key.
//
// The second return says whether the question could be ANSWERED. A caller must
// not read "unknown" as "forbidden": if the directory cannot be reached, every
// integration in the fleet would stop on an outage of a service that is not in
// the request path. It also must not read it as "allowed" without saying so —
// hence two values rather than a silent default here.
func (d *Directory) APIEnabledForTenant(ctx context.Context, tenantID int64) (enabled bool, known bool) {
	tenants, err := d.List(ctx)
	if err != nil {
		return false, false
	}
	for _, t := range tenants {
		if t.ID == tenantID {
			return t.APIEnabled, true
		}
	}
	// Not an FMS tenant as far as karlo_auth is concerned. Unknown, not
	// disabled: the local key still names a real tenant row in this database.
	return false, false
}

// Invalidate drops the cached list so the next List refetches — a company
// notice says the directory has changed; waiting out the TTL would leave a
// newly granted customer invisible to every worker for up to a minute.
func (d *Directory) Invalidate() {
	d.mu.Lock()
	d.cachedAt = time.Time{}
	d.mu.Unlock()
}

// IDs is the shape the background workers want.
func (d *Directory) IDs(ctx context.Context) ([]int64, error) {
	ts, err := d.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out, nil
}

// Name returns a tenant's display name, or "" when it is unknown — which is not
// an error: a caller labelling a row should show the id rather than fail.
func (d *Directory) Name(ctx context.Context, id int64) string {
	ts, err := d.List(ctx)
	if err != nil {
		return ""
	}
	for _, t := range ts {
		if t.ID == id {
			return t.Name
		}
	}
	return ""
}

// TenantForCompany maps a shared-IAM company UUID to the FMS tenant id.
//
// Reports "not found" rather than guessing. An unknown company must not fall
// back to the caller's own tenant: a Karlo staff member acting for a customer
// would then silently read and write somebody else's fleet, which is exactly
// the isolation failure the header exists to make explicit. A company with no
// fms_tenant_id is not an FMS customer and lands here too.
func (d *Directory) TenantForCompany(ctx context.Context, companyID string) (int64, bool) {
	if companyID == "" {
		return 0, false
	}
	ts, err := d.List(ctx)
	if err != nil {
		return 0, false
	}
	for _, t := range ts {
		if t.CompanyID == companyID {
			return t.ID, true
		}
	}
	return 0, false
}

// CompanyForTenant is the reverse of TenantForCompany: the master-data company
// a local tenant id belongs to.
//
// Needed by the write-through. FMS's own screens scope a staff member's action
// with ?account_id=N — a LOCAL bigint — while master data scopes with
// X-Acting-For carrying the company UUID. A write that forwards the header
// alone therefore arrives upstream with no acting-for at all when the operator
// used the query form, and master data files the record under the STAFF
// MEMBER'S OWN company. The vehicle appears in FMS under the customer and in
// TMS under Karlo Platform, and nothing reports the split.
func (d *Directory) CompanyForTenant(ctx context.Context, tenantID int64) (string, bool) {
	if tenantID == 0 {
		return "", false
	}
	ts, err := d.List(ctx)
	if err != nil {
		return "", false
	}
	for _, t := range ts {
		if t.ID == tenantID && t.CompanyID != "" {
			return t.CompanyID, true
		}
	}
	return "", false
}

// fetch asks the authentication service for every FMS company.
//
// Paging lives in the client, which walks to exhaustion. Reading a single page
// would hand the background workers a truncated customer list — correct-looking
// until the deployment outgrows one page, and then silently no webhooks and no
// retention for everyone past it.
func (d *Directory) fetch(ctx context.Context) ([]Tenant, error) {
	companies, err := d.iam.ListFMSCompanies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Tenant, 0, len(companies))
	for _, c := range companies {
		out = append(out, Tenant{
			ID: c.FMSTenantID, Name: c.Name, CompanyID: c.ID, APIEnabled: c.APIEnabled,
		})
	}
	return out, nil
}
