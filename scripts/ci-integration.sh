#!/bin/sh
set -eu

umask 077
: "${FLOWLENS_POSTGRES_ADMIN_URL:?FLOWLENS_POSTGRES_ADMIN_URL is required}"

for command in createdb dropdb go python3; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "ci integration requires $command" >&2
    exit 2
  }
done

admin_url=$FLOWLENS_POSTGRES_ADMIN_URL
databases=
unset PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE PGSSLMODE

assignments=$(
  FLOWLENS_CI_ADMIN_URL=$admin_url python3 -c '
import os
import shlex
import sys
import urllib.parse

source = urllib.parse.urlsplit(os.environ["FLOWLENS_CI_ADMIN_URL"])
if source.scheme not in ("postgres", "postgresql") or not source.hostname or not source.username:
    sys.exit("FLOWLENS_POSTGRES_ADMIN_URL must be a PostgreSQL URL with a host and user")
database = urllib.parse.unquote(source.path.removeprefix("/"))
if not database:
    sys.exit("FLOWLENS_POSTGRES_ADMIN_URL must name a maintenance database")
parameters = urllib.parse.parse_qs(source.query)
values = {
    "PGHOST": source.hostname,
    "PGPORT": str(source.port or 5432),
    "PGUSER": urllib.parse.unquote(source.username),
    "PGPASSWORD": urllib.parse.unquote(source.password or ""),
    "PGDATABASE": database,
    "PGSSLMODE": parameters.get("sslmode", ["prefer"])[-1],
}
for key, value in values.items():
    print(key + "=" + shlex.quote(value))
'
) || exit $?
eval "$assignments"
unset assignments
export PGHOST PGPORT PGUSER PGPASSWORD PGDATABASE PGSSLMODE

cleanup() {
  exit_status=$?
  cleanup_status=0
  trap - EXIT HUP INT TERM
  for database in $databases; do
    dropdb --maintenance-db="$PGDATABASE" --if-exists --force "$database" >/dev/null 2>&1 || cleanup_status=1
  done
  if [ "$cleanup_status" -ne 0 ]; then
    echo "ci integration could not remove every temporary database" >&2
    [ "$exit_status" -ne 0 ] || exit_status=1
  fi
  exit "$exit_status"
}

trap cleanup EXIT
trap 'exit 1' HUP INT TERM

for package in auth retention trafficquery webhook; do
  database="flowlens_ci_${package}_$$"
  databases="$database $databases"
  createdb --maintenance-db="$PGDATABASE" "$database"
  test_url=$(
    FLOWLENS_CI_ADMIN_URL=$admin_url FLOWLENS_CI_DATABASE=$database python3 -c '
import os
import urllib.parse

source = urllib.parse.urlsplit(os.environ["FLOWLENS_CI_ADMIN_URL"])
database = urllib.parse.quote(os.environ["FLOWLENS_CI_DATABASE"], safe="")
print(urllib.parse.urlunsplit((source.scheme, source.netloc, "/" + database, source.query, source.fragment)))
'
  )
  FLOWLENS_TEST_DATABASE_URL=$test_url go test "./internal/server/$package"
done
