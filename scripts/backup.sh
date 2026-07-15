#!/bin/sh
set -eu

umask 077
script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
project_dir=$(dirname "$script_dir")
destination=${1:-"$project_dir/backups"}
environment=${FLOWLENS_SERVER_ENVIRONMENT:-/etc/flowlens/server.env}
pg_dump=${FLOWLENS_PG_DUMP:-pg_dump}
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
base="flowlens-$timestamp"

database_url=${FLOWLENS_DATABASE_URL:-}
if [ -z "$database_url" ] && [ -r "$environment" ]; then
  database_url=$(sed -n 's/^FLOWLENS_DATABASE_URL=//p' "$environment" | tail -n 1)
fi
[ -n "$database_url" ] || { echo "FLOWLENS_DATABASE_URL is required" >&2; exit 1; }
command -v "$pg_dump" >/dev/null 2>&1 || { echo "PostgreSQL dump command is missing" >&2; exit 1; }

mkdir -p "$destination"
chmod 0700 "$destination"
temporary=$(mktemp "$destination/.${base}.XXXXXX")
trap 'rm -f "$temporary"' EXIT HUP INT TERM

PGDATABASE=$database_url "$pg_dump" --format=custom --compress=6 > "$temporary"
mv "$temporary" "$destination/$base.dump"
trap - EXIT HUP INT TERM

{
  printf 'created_at=%s\n' "$timestamp"
  printf 'format=postgresql-custom\n'
  printf 'dump_tool=%s\n' "$("$pg_dump" --version | sed -n '1p')"
} > "$destination/$base.manifest"

(cd "$destination" && sha256sum "$base.dump" "$base.manifest" > "$base.sha256")
chmod 0600 "$destination/$base.dump" "$destination/$base.manifest" "$destination/$base.sha256"
printf '%s\n' "$destination/$base.dump"
