.PHONY: up up-app down migrate migrate-remote seed reset psql redis-cli logs run-api swagger sqlc tidy

# Start local infra (Postgres/PostGIS + Redis)
up:
	docker compose up -d

# Start infra + the API container (compose profile "app")
up-app:
	docker compose --profile app up -d --build

down:
	docker compose down

# Apply SQL migrations in order against the compose DB (as owner `postgres`).
migrate:
	@for f in db/migrations/*.sql; do \
		echo ">> applying $$f"; \
		docker compose exec -T db psql -U postgres -d fms_app -v ON_ERROR_STOP=1 -f - < $$f || exit 1; \
	done
	@echo "migrations applied."

# Apply migrations against $DATABASE_URL via host psql (remote/managed DBs).
migrate-remote:
	@test -n "$$DATABASE_URL" || { echo "usage: DATABASE_URL=postgres://... make migrate-remote"; exit 1; }
	./scripts/migrate.sh

# Seed a default tenant + admin/operator logins.
seed:
	docker compose exec -T db psql -U postgres -d fms_app -v ON_ERROR_STOP=1 -f - < seed/seed.sql
	@echo "seeded (admin@karlo.id / admin123)."

# Wipe volumes and rebuild from scratch (infra + migrate + seed).
reset: down
	docker compose down -v
	$(MAKE) up
	@sleep 6
	$(MAKE) migrate
	$(MAKE) seed

# Run the API service (Swagger UI at http://localhost:8080/swagger/index.html)
run-api:
	@set -a; . ./.env 2>/dev/null || true; set +a; go run ./cmd/api

# Regenerate type-safe DB code from db/migrations + internal/**/queries.sql
sqlc:
	@which sqlc >/dev/null 2>&1 || go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	sqlc generate
	@echo "sqlc code regenerated."

# Regenerate OpenAPI spec from swaggo annotations.
swagger:
	@which swag >/dev/null 2>&1 || go install github.com/swaggo/swag/cmd/swag@latest
	swag init -g main.go -d cmd/api,internal -o cmd/api/docs --parseDependency --parseInternal
	@echo "swagger spec regenerated."

tidy:
	go mod tidy

psql:
	docker compose exec db psql -U postgres -d fms_app

redis-cli:
	docker compose exec redis redis-cli

logs:
	docker compose logs -f
