#!/usr/bin/env bash
#
# Apply db/migrations/*.sql in order against $DATABASE_URL using host `psql`.
# Defaults to the local compose DB (host port 5433). Use for remote/managed DBs:
#
#   DATABASE_URL="postgres://user:pass@host:5432/fms_app?sslmode=require" ./scripts/migrate.sh
#
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
DATABASE_URL="${DATABASE_URL:-postgres://postgres:postgres@localhost:5433/fms_app}"

masked="$(printf '%s' "$DATABASE_URL" | sed -E 's#://[^@]*@#://***@#')"
echo ">> target: $masked"

for f in "$ROOT"/db/migrations/*.sql; do
  echo ">> applying $(basename "$f")"
  psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -q -f "$f"
done

echo ">> migrations applied."
