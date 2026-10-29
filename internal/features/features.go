// Package features implements per-tenant, module-level feature entitlements.
//
// A platform admin toggles which modules each customer may use. Enforcement is
// two-layer: the frontend hides disabled modules, and RequireFeature returns 403
// on the API so a disabled module can't be reached by calling it directly.
//
// Default is OPT-OUT: a tenant with no explicit row for a feature has it ENABLED,
// so existing customers keep everything until an admin turns something off.
package features

import (
	"context"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/karlo/fms-backend/internal/auth"
	"github.com/karlo/fms-backend/internal/platform/db"
	"github.com/karlo/fms-backend/internal/platform/httpx"
)

// Def is one gateable feature. Parent ("" for top-level) nests a sub-feature
// under another, so the admin editor mirrors the app's sidebar hierarchy. Order
// in Registry is the display order.
type Def struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Parent string `json:"parent"`
	// Dev marks a roadmap module that isn't built yet: it appears in the
	// configurator (so an account can pre-enable it) and the sidebar shows it as
	// a stub, but there's no working page behind it yet.
	Dev bool `json:"dev,omitempty"`
}

// Registry is the canonical, ordered list of toggleable features, arranged as
// parent → sub-feature to match the sidebar. Settings and tenant/platform admin
// are intentionally absent (always available so an admin can't lock a customer
// out of their own account).
// The published artifact fms-features.json (module root) is a verbatim copy of
// this Registry, consumed by the shared Karlo IAM to detect drift. Regenerate it
// whenever this list changes:
//
//go:generate go run github.com/karlo/fms-backend/cmd/features-json
var Registry = []Def{
	{Key: "dashboard", Label: "Dashboard"},
	{Key: "ai_dashboard", Label: "AI Dashboard", Parent: "dashboard"},

	{Key: "live", Label: "Live View"},
	{Key: "geofence", Label: "Geofence", Parent: "live"},

	{Key: "controltower", Label: "Control Tower"},
	{Key: "notifications", Label: "Notifications"},

	{Key: "share_link", Label: "Share Link"},
	{Key: "generate_link", Label: "Generate Link", Parent: "share_link"},

	{Key: "reports", Label: "Reporting"},
	{Key: "custom_report", Label: "Custom Report", Parent: "reports"},
	{Key: "ai_reporting", Label: "AI Reporting", Parent: "reports"},

	{Key: "camera", Label: "Camera (TrackVision)"},

	{Key: "fleet_management", Label: "Fleet Management"},
	{Key: "maintenance", Label: "Maintenance", Parent: "fleet_management"},
	{Key: "hangars", Label: "Workshops", Parent: "fleet_management"},
	{Key: "license", Label: "License", Parent: "fleet_management"},

	{Key: "master_data", Label: "Master Data"},
	{Key: "dashcams", Label: "Dashcam", Parent: "master_data"},
	{Key: "rfid", Label: "RFID", Parent: "master_data"},
	{Key: "users", Label: "Users", Parent: "master_data"},

	{Key: "scorecard", Label: "Driver Performance"},
	{Key: "marketplace", Label: "Marketplace"},

	// --- Business modules (roadmap — under development) ---------------------
	// Per-account toggleable so each account enables what fits its flow. The
	// keys mirror the frontend nav (nav.config.tsx): a section key gates the
	// whole group, its children gate individual modules.
	{Key: "sales", Label: "Sales & Orders", Dev: true},
	{Key: "orders", Label: "Orders", Parent: "sales", Dev: true},
	{Key: "invoices", Label: "Invoices", Parent: "sales", Dev: true},
	{Key: "quotations", Label: "Quotations", Parent: "sales", Dev: true},

	{Key: "procurement", Label: "Procurement", Dev: true},
	{Key: "purchase-orders", Label: "Purchase Orders", Parent: "procurement", Dev: true},
	{Key: "purchase-invoices", Label: "Purchase Invoices", Parent: "procurement", Dev: true},
	{Key: "purchase-quotes", Label: "Purchase Quotes", Parent: "procurement", Dev: true},
	{Key: "expenses", Label: "Expenses", Parent: "procurement", Dev: true},

	{Key: "inventory", Label: "Inventory & Products", Dev: true},
	{Key: "products", Label: "Products", Parent: "inventory", Dev: true},
	{Key: "stock", Label: "Inventory", Parent: "inventory", Dev: true},
	{Key: "warehouses", Label: "Warehouses", Parent: "inventory", Dev: true},
	{Key: "fixed-assets", Label: "Fixed Assets", Parent: "inventory", Dev: true},

	{Key: "finance", Label: "Finance", Dev: true},
	{Key: "cash-bank", Label: "Cash & Bank", Parent: "finance", Dev: true},
	{Key: "chart-of-accounts", Label: "Chart of Accounts", Parent: "finance", Dev: true},
	{Key: "cost-centers", Label: "Cost Centers", Parent: "finance", Dev: true},
	{Key: "journal", Label: "Journal Entries", Parent: "finance", Dev: true},
	{Key: "payroll", Label: "Payroll", Parent: "finance", Dev: true},

	{Key: "commerce", Label: "Commerce & CRM", Dev: true},
	{Key: "pos", Label: "Point of Sale", Parent: "commerce", Dev: true},
	{Key: "distribution", Label: "Distribution", Parent: "commerce", Dev: true},
	{Key: "crm", Label: "CRM", Parent: "commerce", Dev: true},
	{Key: "contacts", Label: "Contacts", Parent: "commerce", Dev: true},
}

func valid(key string) bool {
	for _, d := range Registry {
		if d.Key == key {
			return true
		}
	}
	return false
}

// Handler serves the entitlement endpoints and provides the gate middleware.
type Handler struct{ pool *db.Pool }

// NewHandler builds the handler.
func NewHandler(pool *db.Pool) *Handler { return &Handler{pool: pool} }

// Routes mounts the entitlement endpoints inside the authenticated group.
func (h *Handler) Routes(r chi.Router) {
	// Per-tenant entitlements are managed in the shared IAM (Accounts page),
	// not here — the old /v1/tenants/{id}/features pair is gone with it.
	// Any authenticated user may read their OWN tenant's effective features, so
	// the app can hide disabled modules.
	r.Get("/v1/me/features", h.getMine)

	// The registry itself — the FMS feature list and its parent/child nesting,
	// with no per-account state. Split out because entitlement STATE moved to
	// the shared authentication service while the vocabulary did not: the list
	// of what an FMS feature even is belongs to FMS, and duplicating it there
	// would give two registries to keep in step.
	r.Get("/v1/features/registry", h.registry)
}

// overrides returns the explicit tenant_features rows for a tenant (key->enabled).
// `tenant_features` has no RLS, so this reads directly through the pool — the same
// pattern the tenants handler uses for cross-tenant management.
func (h *Handler) overrides(ctx context.Context, tenantID int64) (map[string]bool, error) {
	rows, err := h.pool.Query(ctx, `SELECT feature_key, enabled FROM tenant_features WHERE tenant_id = $1`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var k string
		var en bool
		if err := rows.Scan(&k, &en); err != nil {
			return nil, err
		}
		out[k] = en
	}
	return out, rows.Err()
}

// effective merges the registry (all-on default) with the tenant's overrides.
func effective(ov map[string]bool) map[string]bool {
	out := make(map[string]bool, len(Registry))
	for _, d := range Registry {
		if v, ok := ov[d.Key]; ok {
			out[d.Key] = v
		} else {
			out[d.Key] = true // opt-out default
		}
	}
	return out
}

// Enabled reports whether a tenant has a feature on (default true). Unknown keys
// are treated as enabled so a gate on an unregistered key never hard-blocks.
func (h *Handler) Enabled(ctx context.Context, tenantID int64, key string) bool {
	if !valid(key) {
		return true
	}
	var enabled bool
	err := h.pool.QueryRow(ctx, `SELECT enabled FROM tenant_features WHERE tenant_id = $1 AND feature_key = $2`, tenantID, key).Scan(&enabled)
	if err == pgx.ErrNoRows {
		return true // no explicit row = enabled
	}
	if err != nil {
		return true // fail OPEN: an entitlement lookup error must not break the app
	}
	return enabled
}

// RequireFeature blocks (403) requests whose tenant has `key` disabled. Platform
// admins always pass (they operate across tenants). Attach to a module's routes.
//
// A token from the shared authentication service carries the company's
// entitlement (acc.fms.features, decided on Company Access), and that is
// what decides here — the same answer /v1/me/features gives the sidebar.
// Until 2026-09-21 this read fms_app.tenant_features instead, where nothing
// had been written since the cutover, so a module switched off in the IAM
// vanished from the sidebar but still answered when called directly. The
// table remains only for a locally minted token.
func (h *Handler) RequireFeature(key string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := auth.FromContext(r.Context())
			if ok && auth.AtLeast(c.Role, auth.RolePlatformAdmin) {
				next.ServeHTTP(w, r)
				return
			}
			if ok && c.FromSharedIAM() {
				if !valid(key) || slices.Contains(c.Features, key) {
					next.ServeHTTP(w, r)
					return
				}
				httpx.Error(w, http.StatusForbidden, "feature not enabled for this account")
				return
			}
			if !h.Enabled(r.Context(), auth.TenantID(r.Context()), key) {
				httpx.Error(w, http.StatusForbidden, "feature not enabled for this account")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getMine: the caller's own tenant effective features as {key: bool} — the app
// uses this to hide disabled modules.
func (h *Handler) getMine(w http.ResponseWriter, r *http.Request) {
	// Answer from the TOKEN when it came from the shared authentication
	// service, because that is what the API itself now enforces.
	//
	// This used to read fms_app.tenant_features, where an absent row means
	// enabled — so with entitlements moved, every account came out with all 47
	// features on while the token granted 27. The sidebar offered modules the
	// API would refuse, which is the worst way round: the user finds out by
	// clicking.
	//
	// A ROADMAP STUB IS SHOWN ONLY WHEN SOMEBODY TURNED IT ON.
	//
	// It used to be forced on for everyone, because an absent tenant_features
	// row read as enabled and nobody had ever written a row. So every account
	// saw Point of Sale, Procurement, Inventory, Finance and Commerce & CRM in
	// the sidebar — five modules that do not exist, in a fleet product, looking
	// like somebody had wired the wrong nav in.
	//
	// The shared IAM already excludes roadmap modules from entitlement, so
	// simply answering from the token gives the right default: hidden. Enabling
	// one is then a deliberate grant per account, which is what "let a customer
	// pre-enable what is coming" was always supposed to mean — a decision, not
	// the absence of one.
	if c, ok := auth.FromContext(r.Context()); ok && c.FromSharedIAM() {
		granted := make(map[string]bool, len(c.Features))
		for _, f := range c.Features {
			granted[f] = true
		}
		out := make(map[string]bool, len(Registry))
		for _, d := range Registry {
			out[d.Key] = granted[d.Key]
		}
		httpx.JSON(w, http.StatusOK, out)
		return
	}

	ov, err := h.overrides(r.Context(), auth.TenantID(r.Context()))
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "read features failed")
		return
	}
	httpx.JSON(w, http.StatusOK, effective(ov))
}

// registry godoc
//
//	@Summary	The FMS feature registry (no per-account state)
//	@Tags		features
//	@Produce	json
//	@Success	200	{array}	Def
//	@Router		/v1/features/registry [get]
func (h *Handler) registry(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, Registry)
}
