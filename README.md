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

## Multi-tenancy

Every table carries `tenant_id` and is protected by RLS. The API connects as the
non-owner `fms_app` role; each request runs inside a transaction that sets
`app.current_tenant` (see `internal/platform/db`). Login is the one pre-tenant
lookup (permitted by a bootstrap RLS policy).
