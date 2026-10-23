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
```

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
   registered anywhere.

The two stores are **disjoint**, with one operational consequence worth stating
plainly: Karlo's *Integrasi Eksternal (API)* switch
(`companies.settings.externalApiEnabled`) refuses a company's keys in
`karlo_auth` and has no effect on a key this service issued itself. To keep one
switch in charge of the link, mint a tenant's key from Karlo admin and revoke
any FMS-local key for that tenant here. `issued_here` on the token list says
which is which, and deleting a shared-auth row from this list does not stop
that key working — revoking it in Karlo does.

## Multi-tenancy

Every table carries `tenant_id` and is protected by RLS. The API connects as the
non-owner `fms_app` role; each request runs inside a transaction that sets
`app.current_tenant` (see `internal/platform/db`). Login is the one pre-tenant
lookup (permitted by a bootstrap RLS policy).
