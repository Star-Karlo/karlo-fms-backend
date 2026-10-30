# fms-backend

The FMS **application/platform API** — authentication + RBAC, master-data CRUD
(vehicles, trackers, drivers/employees, fleet groups), and a live-view endpoint
that joins the vehicle registry with positions from the telemetry pipeline.

It is a **separate service** from the telemetry backend (`../backend`), reusing
the same stack (Go 1.25, chi, pgx, sqlc, Swagger) and its own Postgres database
(`fms_app`). It never reads the telemetry hypertable directly — it calls the
telemetry HTTP API (`fms-tracking`) by IMEI for all position data.

## Stack

- Go + go-chi/chi v5, jackc/pgx v5
- **sqlc** for type-safe DB access (SQL is the source of truth: `db/queries/*.sql`)
- PostgreSQL 16 + PostGIS, tenant isolation via **Row-Level Security**
- JWT (golang-jwt v5) access + rotating refresh tokens, bcrypt passwords
- Swagger/OpenAPI via swaggo, served at `/swagger/index.html`

## Quick start (local)

```bash
make up          # start Postgres (:5433) + Redis (:6380) in docker
make migrate     # apply db/migrations/*.sql
make seed        # default tenant + admin@karlo.id / admin123 (and an operator)
make run-api     # go run ./cmd/api  → http://localhost:8080
```

Then open http://localhost:8080/swagger/index.html, `POST /v1/auth/login`,
click **Authorize** with `Bearer <access_token>`, and exercise the API.

Point `TELEMETRY_BASE_URL` at a running ingest service (`:8082`) or
`https://fms-tracking.karlo.id` to populate `GET /v1/live-view`. If telemetry is
unreachable the live-view still returns vehicles as `online:false` (no 500).

## Layout

```
cmd/api/            service entrypoint + generated Swagger docs
db/migrations/      raw SQL migrations (runtime source of truth)
db/schema.sql       trimmed schema for sqlc codegen only
db/queries/         sqlc query definitions
internal/
  platform/         config, httpx, db (tenant-scoped Tx), conv
  auth/             JWT, bcrypt, refresh rotation, middleware, RBAC
  vehicle/ tracker/ driver/ fleetgroup/   master-data CRUD
  telemetry/        HTTP client to the telemetry pipeline
  liveview/         registry ⨝ telemetry join
  store/            sqlc-generated (do not edit)
```

## Regenerate code

```bash
make sqlc      # regenerate internal/store from db/schema.sql + db/queries
make swagger   # regenerate cmd/api/docs from handler annotations
make proto     # buf lint + buf generate for the shared gRPC contracts
```

`make proto` **did nothing at all until 23 Oct 2026**: there was no such
target, so make matched the `proto/` *directory*, decided it was up to date and
printed `make: Nothing to be done for 'proto'.` — which reads exactly like
success. The four TMS services all document `make proto` as the way to
regenerate, so anyone carrying that habit here believed they had regenerated
and committed stale bindings; it was caught while adding
`CompanySettings.external_api_enabled`. The target now matches theirs and is in
`.PHONY`, which is the part whose absence caused the silence. Its equivalent by
hand is `cd proto && buf generate`.

The contracts in `proto/` are **hand-copied** from the TMS repos — five copies
of each file, with nothing in any pipeline comparing them. The one script that
does is `authentication-service/scripts/check-proto-sync.sh`; run it from there,
with `FMS_REPO` pointing here if the repos are not side by side. This repo's
copies currently sit *behind* their owners on `auth.proto` and
`notification.proto`, which proto3 handles safely by ignoring fields it cannot
see.

## Auth & roles

Roles are hierarchical: `admin ⊇ manager ⊇ operator`. Reads require any
authenticated user; master-data writes require `manager+`. Access tokens are
short-lived JWTs; refresh tokens rotate on use with reuse detection.

### Machine callers: how an inbound API token resolves

The external integration group (`liveview.IntegrationRoutes` — by IMEI, or by
the plate painted on the truck) accepts a per-tenant API token **or** a user
JWT, because both happen: the TMS presents its account's key, and a person
trying the same call from the Swagger page is signed in.
`apitoken.AuthenticatorOr` decides which, and the order matters, because two of
the three steps refuse in a way that names the wrong thing.

1. **The prefix is the only discriminator.** The credential is read from
   `X-Api-Key`, or from `Authorization: Bearer …`, and anything not starting
   with the literal `fms_` is handed to the JWT authenticator — so a key
   without the prefix is refused as *missing bearer token* and neither key
   store is ever consulted. The test runs **before** any call to the shared
   auth service, which is why keys minted in `karlo_auth` carry an `fms_`
   prefix of their own: it is what lets one tenant use one key in both
   products.
2. **The shared authentication service is asked first** when `WithIAM` is
   configured. It owns the keys the importer copied into `karlo_auth.api_keys`
   and is authoritative for them — deliberately, since matching a stale local
   copy would let a key revoked upstream keep working here. A key it accepts
   comes back with the company's FMS entitlements and its `fms_tenant_id`,
   which becomes the request's `TenantID`, so RLS scopes every query exactly as
   it would for a signed-in user of that account.
3. **Otherwise the local `api_tokens` table** (`localClaims`). This service
   still issues its own keys from *Settings > API Service*; those are unknown
   upstream, and asking upstream alone refused every one of them — as
   "revoked", which reads as a cancellation rather than a key that was never
   registered anywhere. **The company's API grant is applied on this path
   too** — see below.

The two stores remain **disjoint** by design: the local lookup cannot see the
keys the importer copied into `karlo_auth.api_keys`, because matching a stale
local copy would let a key revoked upstream keep working here. `issued_here` on
the token list says which is which, and deleting a shared-auth row from this
list does not stop that key working — revoking it in Karlo does.

### The Karlo API switch applies to locally issued keys

Until 23 Oct 2026 it did not, and that was the two products disagreeing about
whether a customer had API access: Karlo's *Integrasi Eksternal (API)* switch
(`companies.settings.externalApiEnabled`) is checked upstream in
`validateAPIKey`, so cutting a link stopped every key held there and left every
FMS-issued one working — the half nobody could turn off. The fallback at step 3
is deliberate and stays; what was missing is that it consulted no grant at all.

The grant now arrives on the wire as `CompanySettings.external_api_enabled`
(tag 11, `optional` — absent means **enabled**, because key authentication
predates the switch and a plain `bool` would read every undecided company as
cut off). It is resolved once, in `iamclient`, where only the pointer separates
an unset field from an explicit `false`; carried as
`iamclient.Company.APIEnabled` → `tenantdir.Tenant.APIEnabled`; and answered by
`tenantdir.Directory.APIEnabledForTenant`, which returns `(enabled, known)`.
The directory is cached with the rest of the tenant list and invalidated by the
same company notice `tenantsync` already listens for, so flipping the switch in
Karlo admin takes effect here immediately in practice and within the one-minute
TTL at worst.

`apitoken.grantWithdrawn` states the rule in one place and `localClaims` calls
it rather than repeating it — a test against its own copy of the logic passes
while the real path is wrong. **Three cases, and only the first refuses:**

- **known and disabled** → refuse, and *after* the row is found, so the answer
  still distinguishes a key that does not exist from one whose company's access
  was withdrawn;
- **known and enabled** → allow;
- **not known** → **allow.** The directory being unreachable, or the tenant not
  being an FMS company upstream, is not grounds to refuse a key that resolves
  against a real local row. Reading unknown as forbidden would stop every
  integration in the fleet during an outage of a service that is not even in
  the request path.

A nil resolver — `WithDirectory` never called — leaves local keys exactly as
they were, which is the state of a deployment that has not cut over. Either
side may be deployed first: a field this service does not yet receive arrives
nil and defaults to enabled, which is the old behaviour.

Four tests in `internal/apitoken/grant_test.go`. The Karlo-side treatment is
`MICROSERVICES/docs/authentication/API_KEYS.md` §7.

## Multi-tenancy

Every table carries `tenant_id` and is protected by RLS. The API connects as the
non-owner `fms_app` role; each request runs inside a transaction that sets
`app.current_tenant` (see `internal/platform/db`). Login is the one pre-tenant
lookup (permitted by a bootstrap RLS policy).

## Checks: what CI runs, and what it does not

The gate is the pipeline's **Test stage**, defined in `buildspec.ci.yml`. It
builds, vets, runs the tests with `-race`, lints, and asserts that `go mod
tidy` leaves `go.mod` and `go.sum` untouched. All of it runs inside
`TEST_IMAGE` (`golang:1.25-alpine`, pinned to the Dockerfile), so the Go that
compiles the tests is the Go that builds the image. The Deploy stage does not
start until Test passes.

Lint is golangci-lint, pinned to `GOLANGCI_VERSION` (v1.62.2) by the CodeBuild
project rather than by this repo, so a linter release cannot redden an
untouched commit. The rule set is golangci-lint's defaults plus one exclusion,
in `.golangci.yml`: `unused` is off for `internal/platform/`, because that tree
is copied into the four TMS services and which symbols a copy uses differs per
service. The four TMS services run v2.5.0 with a chosen rule list instead;
aligning FMS to it is 53 findings and its own change.

The check script reaches the container through a shell variable filled by a
quoted heredoc, and `internal/ci/buildspec_test.go` fails the build if anyone
inlines it between single quotes again. That is not a style rule: inline, an
apostrophe in a comment ends the quoted argument, and on 30 Oct 2026 it moved
the lint and tidy steps out of the container and onto the CodeBuild host with
nothing in the output saying so.

`.github/workflows/ci.yml` still runs build and vet on every push to `main`,
duplicating two of the steps above.

Deploying is the pipeline's Deploy stage, which runs once Test passes. A green
pipeline is not proof the new code is running — check the running ECS task's
image tag. `.github/workflows/deploy.yml` is still present so an old manual run
can be reproduced.
