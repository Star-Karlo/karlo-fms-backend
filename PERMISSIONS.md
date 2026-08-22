# FMS — Authorization Model & Permission Catalogue

Source of truth for **how FMS decides who may do what**, as built today. Kept in
sync with `internal/auth`, `internal/roles`, `internal/perms`, `internal/features`.
The shared cross-product catalogue links here rather than duplicating it, so the
two can't drift.

> Status: this documents FMS **as it exists in production**. A migration onto the
> shared Karlo IAM (RS256 tokens, one login for TMS+FMS) is proposed but **not
> adopted** — nothing here changes until that is greenlit.

## The model in one line

**Effective access = the company holds the FEATURE _and_ the user's role grants
the PERMISSION KEY.** Two independent tiers, intersected:

1. **Company entitlement** — `tenant_features(tenant_id, feature_key, enabled)`.
   What Karlo staff sold this company. Opt-out default: **no row = enabled**.
2. **User permission** — the user's assigned custom role's `permissions[]` (a list
   of catalogue keys). What this person may do.

A key only bites if its gating feature is held. A key absent from the catalogue
grants nothing.

## Tenancy

- The company record is `tenants(id bigint, name, slug)`. `id` is
  `bigint GENERATED ALWAYS AS IDENTITY` (small dense integers), **not a UUID**.
- Isolation is **Postgres Row-Level Security** on `tenant_id`, set per request via
  `app.current_tenant`. Master data (vehicles, drivers, …) is scoped by the
  token's tenant, never a request body.
- Tokens today are **HS256 (HMAC), self-issued**; claims carry `sub` (user id),
  `role`, `tenant_id`. Entitlements/permissions are **looked up per request**
  (cheap RLS-scoped reads), not embedded in the token.

## Roles — a RANK hierarchy, not permission bundles

`operator (1) < manager (2) < admin (3) < platform_admin (4)` (`auth.AtLeast`).

**Role and permissions are orthogonal in FMS.** Rank does NOT carry a default key
set. Rank alone gates only a few endpoints (e.g. `users.manage`/`roles.manage`
need admin+; tenant provisioning / cross-tenant need `platform_admin`).

| Role | Meaning |
|---|---|
| `operator` | Read-mostly fleet user. |
| `manager` | Operator + acts on master data / alerts. |
| `admin` | Company administrator — user & role management, API tokens. |
| `platform_admin` | **Karlo staff**, cross-tenant; **bypasses company entitlement**. Not a tenant role. |

Permission KEYS are used only by **admin-defined custom roles**: a per-company
`roles` table of `{name, permissions[]}` assigned via `users.role_id`.

**Effective rule today:**
- User **with** a custom role → exactly that role's keys (∩ the company's features).
- User with **no** custom role → **all** catalogue keys (deliberate legacy
  fallback), still bounded by rank on the few endpoints above and by features.

> **OPEN (Nathan's call):** under the shared IAM, replace the "no-role = all keys"
> fallback with explicit per-role default bundles? Not decided.

## "Root" / unrestricted-within-entitlement

FMS has **no explicit `root`/`is_root` flag.** The equivalent is `role = admin`
with **no custom role** assigned → all catalogue keys via the fallback, still
bounded by the company's features. Semantics: *"no explicit keys needed, still
cannot reach a module the company hasn't bought."*

Under the shared IAM, `root` (the single registering `mainAccount`) maps to
"grant all catalogue keys, still feature-gated" — but FMS **keeps its
admin+no-custom-role path as well**; a company may have several rank-admins who
are not the registering account and must not be demoted.

## Sellable features (entitlements)

What a Karlo admin grants a company. Parent → child nesting; opt-out default.

| Feature | Grants |
|---|---|
| `dashboard` | Composable KPI dashboard (live fleet status, widgets). |
| `ai_dashboard` | AI-composed dashboards + natural-language "build with AI". |
| `live` | Live map tracking + trip history + fuel/cost views. |
| `geofence` | Geofences (zones, enter/exit alerts). |
| `controltower` | Operations Center — per-vehicle action/alert queue. |
| `notifications` | Alert events log + notification rule engine. |
| `share_link` | Public, time-limited vehicle tracking links. |
| `generate_link` | Create/issue share links. |
| `reports` | Fleet reports catalogue + CSV export. |
| `custom_report` | Custom / combined report builder. |
| `ai_reporting` | AI-written report summaries. |
| `camera` | Dashcam live / recordings / evidence (TrackVision). |
| `fleet_management` | Parent: maintenance / workshops / documents. |
| `maintenance` | Service schedules & status. |
| `hangars` | Workshops / depots. |
| `license` | Vehicle / driver document expiry. |
| `master_data` | Parent: the registry. |
| `dashcams` | Link camera devices to vehicles. |
| `rfid` | RFID tags / readers (driver identification). |
| `users` | Login-account management. |
| `scorecard` | Driver Performance scoring (0–100). |
| `marketplace` | Add-ons / hardware / spare parts. |

**Not live** (roadmap stubs, `Dev=true`, no permission keys — not sellable yet):
the `sales`/`procurement`/`inventory`/`finance`/`commerce` trees
(orders, invoices, purchase-*, products, stock, cash-bank, chart-of-accounts,
payroll, pos, crm, …).

## Permission catalogue (the keys FMS checks)

Flat dotted `module.action` keys. **Gating feature** is what must be held for the
key to bite — note it is **not** the key prefix. `(none)` = available to any
company that has FMS (no entitlement gate).

| Key | Gating feature | Group | Label |
|---|---|---|---|
| `dashboard.view` | `dashboard` | Dashboard | View dashboard |
| `dashboard.ai` | `ai_dashboard` | Dashboard | AI dashboard |
| `live.view` | `live` | Live Tracking | View live tracking |
| `live.history` | `live` | Live Tracking | View trip history |
| `fuel.view` | `live` | Fuel | View fuel & cost |
| `fuel.price_calc` | `live` | Fuel | Fuel price calculation |
| `geofence.view` | `geofence` | Geofence | View geofences |
| `geofence.manage` | `geofence` | Geofence | Manage geofences |
| `controltower.view` | `controltower` | Control Tower | View control tower |
| `notifications.view` | `notifications` | Notifications | View notifications |
| `notifications.manage` | `notifications` | Notifications | Manage notification rules |
| `reports.view` | `reports` | Reporting | View reports |
| `reports.custom` | `custom_report` | Reporting | Custom report builder |
| `reports.ai` | `ai_reporting` | Reporting | AI reporting |
| `camera.view` | `camera` | Camera | View cameras (TrackVision) |
| `share_link.view` | `share_link` | Share Link | View share links |
| `share_link.create` | `generate_link` | Share Link | Generate share link |
| `maintenance.view` | `maintenance` | Fleet Management | View maintenance |
| `maintenance.manage` | `maintenance` | Fleet Management | Manage maintenance |
| `hangars.view` | `hangars` | Fleet Management | View workshops |
| `hangars.manage` | `hangars` | Fleet Management | Manage workshops |
| `license.view` | `license` | Fleet Management | View licenses |
| `license.manage` | `license` | Fleet Management | Manage licenses |
| `vehicles.view` | (none) | Master Data | View vehicles |
| `vehicles.edit` | (none) | Master Data | Add / edit vehicles |
| `drivers.view` | (none) | Master Data | View drivers |
| `drivers.edit` | (none) | Master Data | Add / edit drivers |
| `rfid.manage` | `rfid` | Master Data | Manage RFID |
| `dashcams.manage` | `camera` | Master Data | Manage dashcams |
| `users.manage` | (none) | Administration | Manage users |
| `roles.manage` | (none) | Administration | Manage roles |

31 keys. This table is generated from `internal/perms/perms.go` — update both
together.
