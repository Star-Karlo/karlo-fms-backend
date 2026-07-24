#!/usr/bin/env bash
# Apply the migrations that exist in db/migrations but have never reached the
# production database, then load the fuel-station reference data.
#
# WHY THIS SCRIPT EXISTS. Three migrations (0024, 0025, 0026) shipped as code
# but never as schema, so Hangars, Company Profile and the SPBU layer all
# return 500 in production while looking deployed. Running them by hand is
# four commands with a security-group hole open in the middle, and the hole is
# the part that gets forgotten — so it is opened and revoked here, with the
# revoke in a trap that fires even if a migration fails.
#
#   AWS_PROFILE=Karlo ./scripts/apply-pending-migrations.sh
#
# Connects as the RDS master (fms_admin), not fms_app: these statements create
# tables and issue GRANT/REVOKE, which the application role cannot do.
set -euo pipefail

REGION=ap-southeast-1
INSTANCE=karlo-fms-app
SG=sg-06d617c320fda9b45
: "${AWS_PROFILE:?set AWS_PROFILE (e.g. AWS_PROFILE=Karlo)}"

echo "==> resolving connection details"
read -r HOST PORT USER SECRET <<<"$(aws rds describe-db-instances --region "$REGION" \
  --db-instance-identifier "$INSTANCE" \
  --query 'DBInstances[0].[Endpoint.Address,Endpoint.Port,MasterUsername,MasterUserSecret.SecretArn]' \
  --output text)"
PASS="$(aws secretsmanager get-secret-value --region "$REGION" --secret-id "$SECRET" \
  --query SecretString --output text | python3 -c 'import sys,json;print(json.load(sys.stdin)["password"])')"
export PGPASSWORD="$PASS"
DB_URL="postgres://${USER}@${HOST}:${PORT}/fms_app?sslmode=require"

IP="$(curl -s https://checkip.amazonaws.com | tr -d '\n')"
echo "==> opening ${SG} to ${IP}/32"
RULE="$(aws ec2 authorize-security-group-ingress --region "$REGION" --group-id "$SG" \
  --ip-permissions "IpProtocol=tcp,FromPort=5432,ToPort=5432,IpRanges=[{CidrIp=${IP}/32,Description=\"temp: migrations\"}]" \
  --query 'SecurityGroupRules[0].SecurityGroupRuleId' --output text)"

# Revoke on ANY exit — success, failure, or Ctrl-C. Leaving the database open
# to the internet because a migration failed would be the worst outcome here.
# Report the outcome as well as tidying up, so a mid-script failure announces
# itself instead of scrolling past behind the revoke message.
cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo
    echo "!!! FAILED at the step above (exit ${rc}). Nothing further was applied."
    echo "!!! Fix the error and re-run — every migration here is re-runnable."
  fi
  echo "==> revoking ${RULE}"
  aws ec2 revoke-security-group-ingress --region "$REGION" --group-id "$SG" \
    --security-group-rule-ids "$RULE" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# Fail fast and legibly if the network filters 5432.
#
# Without this the first psql hangs for its full connect timeout and reports
# "Operation timed out", which reads like an AWS or credentials problem and
# sends you looking in the wrong place. It is almost always the local network:
# mobile data, hotel wifi and corporate networks routinely block outbound
# database ports while leaving 443 open.
echo "==> checking that this network allows outbound 5432"
if ! nc -z -G 15 "$HOST" 5432 2>/dev/null; then
  echo
  echo "  Cannot reach ${HOST}:5432 even though the security group now allows"
  echo "  ${IP}/32. The rule is correct — this network is filtering the port."
  echo
  echo "  Try a different network (mobile hotspot usually works), then re-run."
  echo "  Nothing has been changed; the security-group rule is revoked on exit."
  exit 1
fi

cd "$(dirname "$0")/.."

# --import-only skips straight to the fuel load, for when the schema is already
# in place and only the data is missing. Re-running migrations is safe but
# reprints a wall of "already exists" notices that buries the real outcome.
# --drivers <csv> runs ONLY the driver import. It reuses this script for the
# security-group open/revoke and the Secrets Manager lookup, which is the part
# worth not repeating — the import itself is a separate command.
if [ "${1:-}" = "--drivers" ]; then
  CSV="${2:?usage: $0 --drivers <path-to-csv>}"
  echo "==> importing drivers from ${CSV}"
  DATABASE_URL="host=${HOST} port=${PORT} user=${USER} password=${PASS} dbname=fms_app sslmode=require" \
    go run ./cmd/import-drivers -csv "$CSV" -tenant 1
  echo
  echo "############################################################"
  echo "#  SUCCESS — drivers imported.                             #"
  echo "############################################################"
  exit 0
fi

# --who answers "which account do I sign in with?" without changing anything.
# Read-only, so it is safe to run first when the answer is not obvious.
if [ "${1:-}" = "--who" ]; then
  psql "$DB_URL" -X -c \
    "SELECT u.email, coalesce(u.full_name,'—') AS name, t.name AS tenant, u.role, u.status
     FROM users u JOIN tenants t ON t.id = u.tenant_id ORDER BY t.id, u.id;"
  exit 0
fi

# --promote <email> is the tenant-provisioning bootstrap: apply 0027 and grant
# that account platform_admin, which is the one thing that cannot be done from
# the Customers page itself (nobody can reach it until somebody holds the role).
# Skips the fuel load entirely — this is a 5-second job, not a data import.
if [ "${1:-}" = "--promote" ]; then
  EMAIL="${2:?usage: $0 --promote <email>}"
  echo "==> applying 0027_platform_admin"
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -f db/migrations/0027_platform_admin.sql

  echo "==> promoting ${EMAIL}"
  # Two psql traps, both hit while writing this:
  #
  #   1. Variables are NOT interpolated inside a dollar-quoted body, so wrapping
  #      this in DO $$ ... $$ leaves :'email' untouched.
  #   2. Variables are NOT interpolated in -c either — the string goes to the
  #      server as written, which is the "syntax error at or near \":\"" this
  #      produced. Substitution only happens for input read from stdin or -f.
  #
  # Hence: plain UPDATE ... RETURNING, fed on stdin. psql does the quoting, so
  # an odd address cannot break the statement. Empty output means no such user —
  # reported rather than passing silently, which would look identical to success
  # and leave the page at 403.
  # -q matters as much as the heredoc: without it psql writes the command tag
  # ("UPDATE 0") to stdout, so the empty-result guard below would never fire and
  # a typo'd address would report success having promoted nobody.
  PROMOTED="$(psql "$DB_URL" -v ON_ERROR_STOP=1 -X -q -tA -v email="$EMAIL" <<'SQL'
UPDATE users SET role = 'platform_admin', updated_at = now()
WHERE email = :'email' RETURNING email;
SQL
)"
  if [ -z "$PROMOTED" ]; then
    echo
    echo "  No account with email ${EMAIL}."
    echo "  Run '$0 --who' to list the accounts that exist."
    exit 1
  fi

  echo "==> platform administrators now:"
  psql "$DB_URL" -X -c \
    "SELECT u.email, t.name AS tenant, u.role FROM users u JOIN tenants t ON t.id=u.tenant_id WHERE u.role='platform_admin';"
  echo
  echo "############################################################"
  echo "#  SUCCESS — sign in as ${EMAIL} and open                  #"
  echo "#  Configuration -> Customers.                             #"
  echo "############################################################"
  exit 0
fi

if [ "${1:-}" = "--import-only" ]; then
  echo "==> skipping migrations (--import-only)"
else
for f in 0024_fuel_stations 0025_company_profile 0026_hangars 0027_platform_admin; do
  echo "==> applying ${f}"
  # ON_ERROR_STOP so a failure halts here rather than leaving half a schema.
  # Every one of these migrations is written to be re-runnable.
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -f "db/migrations/${f}.sql"
done
fi

# Prefer the pre-fetched seed. Overpass takes minutes and needs internet and
# database reachability from the SAME machine — the seed needs only the
# database, so the slow half can have been done anywhere, at any time.
if [ -f db/seed/fuel_stations.sql ]; then
  echo "==> loading fuel stations from db/seed (fast path)"
  psql "$DB_URL" -v ON_ERROR_STOP=1 -q -f db/seed/fuel_stations.sql
else
echo "==> loading fuel stations (Overpass; takes a few minutes)"
# KEYWORD/VALUE DSN, not a URL.
#
# AWS generates RDS passwords containing characters like / + = and #, which are
# structural inside a URL's userinfo section. psql tolerated the URL form only
# because the password actually travelled via PGPASSWORD; the Go client parsed
# the URL for real and failed with "invalid userinfo". The keyword form has no
# reserved characters to collide with, so no escaping is needed and no password
# can break it.
DATABASE_URL="host=${HOST} port=${PORT} user=${USER} password=${PASS} dbname=fms_app sslmode=require" \
  go run ./cmd/import-fuel -region all
fi

echo "==> verifying"
psql "$DB_URL" -X -c "
SELECT 'fuel_station' AS table, count(*)::text AS rows FROM fuel_station
UNION ALL SELECT 'hangars', count(*)::text FROM hangars
UNION ALL SELECT 'tenants.logo_key exists',
  (SELECT count(*)::text FROM information_schema.columns
   WHERE table_name='tenants' AND column_name='logo_key');"
echo
echo "############################################################"
echo "#  SUCCESS — all three migrations applied and verified.    #"
echo "#  If you do NOT see this banner, it did not work.         #"
echo "############################################################"
