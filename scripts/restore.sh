#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 /path/to/flowlens-TIMESTAMP.dump" >&2
  exit 2
fi
test_root=${FLOWLENS_RESTORE_TEST_ROOT:-}
if [ -z "$test_root" ] && [ "$(id -u)" -ne 0 ]; then
  echo "restore must run as root" >&2
  exit 1
fi
[ -z "$test_root" ] || [ -d "$test_root" ] || { echo "restore test root is missing" >&2; exit 1; }

dump=$(CDPATH='' cd -- "$(dirname "$1")" && pwd)/$(basename "$1")
directory=$(dirname "$dump")
name=$(basename "$dump" .dump)
checksum="$directory/$name.sha256"
environment=${FLOWLENS_SERVER_ENVIRONMENT:-/etc/flowlens/server.env}
pg_restore=${FLOWLENS_PG_RESTORE:-pg_restore}
psql=${FLOWLENS_PSQL:-psql}
systemctl=${FLOWLENS_SYSTEMCTL:-systemctl}
curl=${FLOWLENS_CURL:-curl}
sha256sum=${FLOWLENS_SHA256SUM:-sha256sum}
sleep=${FLOWLENS_SLEEP:-sleep}
health_attempts=${FLOWLENS_RESTORE_HEALTH_ATTEMPTS:-60}
health_url=${FLOWLENS_HEALTH_URL:-http://127.0.0.1:8088/healthz}

database_url=${FLOWLENS_DATABASE_URL:-}
if [ -z "$database_url" ] && [ -r "$environment" ]; then
  database_url=$(sed -n 's/^FLOWLENS_DATABASE_URL=//p' "$environment" | tail -n 1)
fi
[ -n "$database_url" ] || { echo "FLOWLENS_DATABASE_URL is required" >&2; exit 1; }
case "$health_attempts" in *[!0-9]*|''|0) echo "FLOWLENS_RESTORE_HEALTH_ATTEMPTS must be a positive integer" >&2; exit 1;; esac
for command in "$pg_restore" "$psql" "$curl" "$systemctl" "$sha256sum" "$sleep"; do
  command -v "$command" >/dev/null 2>&1 || { echo "required restore command is missing" >&2; exit 1; }
done

test -f "$dump" || { echo "backup dump is missing" >&2; exit 1; }
test -f "$checksum" || { echo "backup checksum is missing" >&2; exit 1; }
(cd "$directory" && "$sha256sum" -c "$name.sha256")

require_stopped_service() {
  unit=$1
  if ! state=$("$systemctl" show "$unit" --property=ActiveState --value 2>/dev/null); then
    echo "could not determine $unit state" >&2
    exit 1
  fi
  case "$state" in
    inactive|failed) return ;;
    active|activating|deactivating|reloading|maintenance)
      echo "stop $unit before restore (state: $state)" >&2
      exit 1
      ;;
    *)
      echo "refusing restore with unknown $unit state" >&2
      exit 1
      ;;
  esac
}

require_stopped_service flowlens-server.service
require_stopped_service flowlens-agent.service

tables=$(PGDATABASE=$database_url "$psql" --no-psqlrc --tuples-only --no-align --command="SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace AND relkind IN ('r','p')")
if [ "$tables" != 0 ]; then
  echo "target database must be empty" >&2
  exit 1
fi

if ! PGDATABASE=$database_url "$pg_restore" --dbname= --single-transaction --exit-on-error --no-owner --no-privileges "$dump"; then
  echo "backup archive could not be applied atomically" >&2
  exit 1
fi

"$systemctl" start flowlens-server.service
attempt=0
until "$curl" -fsS "$health_url" >/dev/null 2>&1; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt "$health_attempts" ] || { echo "flowlens-server.service did not become healthy" >&2; exit 1; }
  "$sleep" 1
done
"$systemctl" start flowlens-agent.service
"$systemctl" is-active --quiet flowlens-server.service
"$systemctl" is-active --quiet flowlens-agent.service
printf '%s\n' "restore completed and services are healthy"
