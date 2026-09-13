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
		out = append(out, Tenant{ID: c.FMSTenantID, Name: c.Name})
	}
	return out, nil
}
