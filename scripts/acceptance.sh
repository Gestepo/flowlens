#!/bin/sh
set -eu

repo_root=$(CDPATH='' cd -- "$(dirname "$0")/.." && pwd)
public_url=${FLOWLENS_URL:-https://monitor.example.com}
local_url=${FLOWLENS_LOCAL_URL:-http://127.0.0.1:8088}
node_id=${FLOWLENS_NODE_ID:-flowlens-node-1}
admin_username=${FLOWLENS_ADMIN_USERNAME:-admin}
password_file=${FLOWLENS_ADMIN_PASSWORD_FILE:-/etc/flowlens/admin-password}
report=${FLOWLENS_ACCEPTANCE_REPORT:-/var/lib/flowlens/acceptance-report.md}
run_browser=${FLOWLENS_ACCEPTANCE_BROWSER:-1}
postgres_adapter=${FLOWLENS_POSTGRES_ADAPTER:-native}
server_environment=${FLOWLENS_SERVER_ENVIRONMENT:-/etc/flowlens/server.env}
psql_command=${FLOWLENS_PSQL:-psql}
createdb_command=${FLOWLENS_CREATEDB:-createdb}
dropdb_command=${FLOWLENS_DROPDB:-dropdb}
postgres_database=${FLOWLENS_DATABASE_NAME:-flowlens}
postgres_admin_user=${FLOWLENS_POSTGRES_ADMIN_USER:-postgres}
accuracy_duration=${FLOWLENS_ACCURACY_DURATION_SECONDS:-600}
accuracy_limit=${FLOWLENS_ACCURACY_LIMIT_PERCENT:-2}
concurrent_connections=${FLOWLENS_CONCURRENT_CONNECTIONS:-1000}
interface=${FLOWLENS_CAPTURE_INTERFACE:-$(awk -F= '/^FLOWLENS_CAPTURE_INTERFACES=/{print $2; exit}' /etc/flowlens/agent.env | cut -d, -f1)}
tmp_dir=$(mktemp -d)
cookie_file=$tmp_dir/cookies
health_file=$tmp_dir/health.json
nodes_file=$tmp_dir/nodes.json
latencies_file=$tmp_dir/query-latencies.txt
traffic_pid=
load_pid=
test_database=

database_url=${FLOWLENS_DATABASE_URL:-}
if [ -z "$database_url" ] && [ -r "$server_environment" ]; then
  database_url=$(sed -n 's/^FLOWLENS_DATABASE_URL=//p' "$server_environment" | tail -n 1)
fi
[ -n "$database_url" ] || { echo "FLOWLENS_DATABASE_URL is required" >&2; exit 1; }
admin_database_url=${FLOWLENS_POSTGRES_ADMIN_URL:-}
postgres_admin_host=
postgres_admin_port=
postgres_admin_user=
postgres_admin_password=
postgres_admin_name=
postgres_admin_sslmode=
postgres_database_host=
postgres_database_port=
postgres_database_user=
postgres_database_password=
postgres_database_name=
postgres_database_sslmode=

postgres_create_database() {
  case "$postgres_adapter" in
    native)
      PGHOST=$postgres_admin_host PGPORT=$postgres_admin_port PGUSER=$postgres_admin_user \
        PGPASSWORD=$postgres_admin_password PGDATABASE=$postgres_admin_name PGSSLMODE=$postgres_admin_sslmode \
        "$createdb_command" --owner="${FLOWLENS_TEST_DATABASE_OWNER:-flowlens}" "$1"
      ;;
    docker) docker exec "$FLOWLENS_POSTGRES_CONTAINER" createdb -U "$postgres_admin_user" -O "${FLOWLENS_TEST_DATABASE_OWNER:-flowlens}" "$1" ;;
  esac
}

postgres_drop_database() {
  case "$postgres_adapter" in
    native)
      PGHOST=$postgres_admin_host PGPORT=$postgres_admin_port PGUSER=$postgres_admin_user \
        PGPASSWORD=$postgres_admin_password PGDATABASE=$postgres_admin_name PGSSLMODE=$postgres_admin_sslmode \
        "$dropdb_command" --if-exists --force "$1"
      ;;
    docker) docker exec "$FLOWLENS_POSTGRES_CONTAINER" dropdb -U "$postgres_admin_user" --if-exists --force "$1" ;;
  esac
}

postgres_query() {
  case "$postgres_adapter" in
    native)
      PGHOST=$postgres_database_host PGPORT=$postgres_database_port PGUSER=$postgres_database_user \
        PGPASSWORD=$postgres_database_password PGDATABASE=$postgres_database_name PGSSLMODE=$postgres_database_sslmode \
        "$psql_command" --no-psqlrc --tuples-only --no-align --command="$1"
      ;;
    docker) docker exec "$FLOWLENS_POSTGRES_CONTAINER" psql -U "$postgres_admin_user" -d "$postgres_database" -Atc "$1" ;;
  esac
}

cleanup() {
  [ -z "$traffic_pid" ] || kill "$traffic_pid" >/dev/null 2>&1 || true
  [ -z "$load_pid" ] || kill "$load_pid" >/dev/null 2>&1 || true
  if [ -n "$test_database" ]; then
    postgres_drop_database "$test_database" >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

for command in curl dd git go make node npm npx python3 sha256sum systemctl; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command is missing: $command" >&2; exit 1; }
done
case "$postgres_adapter" in
  native)
    [ -n "$admin_database_url" ] || { echo "FLOWLENS_POSTGRES_ADMIN_URL is required for the native PostgreSQL adapter" >&2; exit 1; }
    [ "$admin_database_url" != "$database_url" ] || { echo "FLOWLENS_POSTGRES_ADMIN_URL must differ from FLOWLENS_DATABASE_URL" >&2; exit 1; }
    for command in "$psql_command" "$createdb_command" "$dropdb_command"; do
      command -v "$command" >/dev/null 2>&1 || { echo "required PostgreSQL command is missing" >&2; exit 1; }
    done
    postgres_admin_assignments=$(
      FLOWLENS_ACCEPTANCE_ADMIN_URL=$admin_database_url FLOWLENS_ACCEPTANCE_DATABASE_URL=$database_url python3 -c '
import os
import shlex
import urllib.parse

def emit(prefix, raw, label):
    source = urllib.parse.urlsplit(raw)
    if source.scheme not in ("postgres", "postgresql") or not source.hostname or not source.username:
        raise SystemExit(label + " must be a PostgreSQL URL with a host and user")
    database = urllib.parse.unquote(source.path.removeprefix("/"))
    if not database:
        raise SystemExit(label + " must name a database")
    parameters = urllib.parse.parse_qs(source.query)
    values = {
        prefix + "_host": source.hostname,
        prefix + "_port": str(source.port or 5432),
        prefix + "_user": urllib.parse.unquote(source.username),
        prefix + "_password": urllib.parse.unquote(source.password or ""),
        prefix + "_name": database,
        prefix + "_sslmode": parameters.get("sslmode", ["prefer"])[-1],
    }
    for key, value in values.items():
        print(key + "=" + shlex.quote(value))

emit("postgres_admin", os.environ["FLOWLENS_ACCEPTANCE_ADMIN_URL"], "FLOWLENS_POSTGRES_ADMIN_URL")
emit("postgres_database", os.environ["FLOWLENS_ACCEPTANCE_DATABASE_URL"], "FLOWLENS_DATABASE_URL")
'
    ) || exit $?
    eval "$postgres_admin_assignments"
    unset postgres_admin_assignments
    postgres_host=
    ;;
  docker)
    [ -n "${FLOWLENS_POSTGRES_CONTAINER:-}" ] || { echo "FLOWLENS_POSTGRES_CONTAINER is required for the Docker PostgreSQL adapter" >&2; exit 1; }
    command -v docker >/dev/null 2>&1 || { echo "Docker is required for the selected PostgreSQL adapter" >&2; exit 1; }
    postgres_host=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$FLOWLENS_POSTGRES_CONTAINER")
    ;;
  *) echo "FLOWLENS_POSTGRES_ADAPTER must be native or docker" >&2; exit 1 ;;
esac
[ -r "$password_file" ] || { echo "administrator password file is not readable: $password_file" >&2; exit 1; }
case "$interface" in *[!A-Za-z0-9_.:-]*|'') echo "invalid capture interface" >&2; exit 1;; esac
case "$node_id" in *[!A-Za-z0-9_.:-]*|'') echo "invalid node identifier" >&2; exit 1;; esac
case "$accuracy_duration:$concurrent_connections" in *[!0-9:]*|:*|*:) echo "acceptance duration and connection count must be integers" >&2; exit 1;; esac

test_database="flowlens_acceptance_$$"
postgres_create_database "$test_database"
test_database_url=$(FLOWLENS_BASE_DATABASE_URL=$database_url FLOWLENS_TEST_DATABASE_NAME=$test_database FLOWLENS_POSTGRES_HOST=$postgres_host python3 -c '
import os, urllib.parse
source=urllib.parse.urlsplit(os.environ["FLOWLENS_BASE_DATABASE_URL"])
username=urllib.parse.quote(source.username or "", safe="")
password=urllib.parse.quote(source.password or "", safe="")
host=os.environ["FLOWLENS_POSTGRES_HOST"] or source.hostname or ""
netloc=f"{username}:{password}@{host}:{source.port or 5432}"
print(urllib.parse.urlunsplit((source.scheme, netloc, "/"+os.environ["FLOWLENS_TEST_DATABASE_NAME"], source.query, "")))
')
FLOWLENS_TEST_DATABASE_URL=$test_database_url FLOWLENS_QUERY_PLAN_ROWS=1000000 \
  go test -p 1 -race -v ./internal/server/auth ./internal/server/retention \
    ./internal/server/trafficquery ./internal/server/webhook >"$tmp_dir/integration-tests.log"
query_plan_evidence=$(sed -n 's/^.*query-plan evidence: //p' "$tmp_dir/integration-tests.log" | tail -n 1)
[ -n "$query_plan_evidence" ] || { echo "million-row query-plan evidence is missing" >&2; exit 1; }
integration_evidence="authentication, retention, traffic query, and Webhook integration fixtures passed against disposable PostgreSQL"

password=$(cat "$password_file")
FLOWLENS_LOGIN_USERNAME=$admin_username FLOWLENS_LOGIN_PASSWORD=$password python3 -c '
import json, os
print(json.dumps({"username": os.environ["FLOWLENS_LOGIN_USERNAME"], "password": os.environ["FLOWLENS_LOGIN_PASSWORD"]}))
' | curl -fsS --cookie-jar "$cookie_file" -H 'Content-Type: application/json' --data-binary @- "$public_url/api/v1/auth/login" >/dev/null
unset password

session=$(curl -fsS --cookie "$cookie_file" "$public_url/api/v1/session")
printf '%s' "$session" | python3 -c 'import json,sys; data=json.load(sys.stdin); assert data.get("authenticated") is True and data.get("username")'
unauthorized_status=$(curl -sS -o /dev/null -w '%{http_code}' "$public_url/api/v1/nodes")
[ "$unauthorized_status" = 401 ] || { echo "protected API returned $unauthorized_status without a session" >&2; exit 1; }

curl -fsS "$local_url/healthz" >/dev/null
curl -fsS --cookie "$cookie_file" "$public_url/api/v1/health" >"$health_file"
curl -fsS --cookie "$cookie_file" "$public_url/api/v1/nodes" >"$nodes_file"
systemctl is-active --quiet flowlens-agent
systemctl is-active --quiet flowlens-server

ingestion_lag=$(python3 - "$nodes_file" "$node_id" <<'PY'
import datetime, json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    nodes = json.load(source).get("items", [])
node = next((item for item in nodes if item.get("id") == sys.argv[2]), None)
if not node:
    raise SystemExit("configured node is missing")
seen = datetime.datetime.fromisoformat(node["last_seen_at"].replace("Z", "+00:00"))
print(f"{(datetime.datetime.now(datetime.timezone.utc) - seen).total_seconds():.3f}")
PY
)
python3 - "$ingestion_lag" <<'PY'
import sys
lag=float(sys.argv[1])
if lag < 0 or lag > 5:
    raise SystemExit(f"ingestion lag is outside 0..5 seconds: {lag}")
PY

: >"$latencies_file"
query=0
while [ "$query" -lt 20 ]; do
  curl -fsS --cookie "$cookie_file" -o /dev/null -w '%{time_total}\n' \
    "$public_url/api/v1/overview?node=$node_id&range=30d" >>"$latencies_file"
  query=$((query + 1))
done
query_latency=$(python3 - "$latencies_file" <<'PY'
import math, statistics, sys
values=sorted(float(line) for line in open(sys.argv[1], encoding="utf-8") if line.strip())
if len(values) != 20:
    raise SystemExit("query latency sample is incomplete")
p95=values[math.ceil(len(values)*.95)-1]
if p95 > 2:
    raise SystemExit(f"30-day query p95 exceeds two seconds: {p95}")
print(f"p50={statistics.median(values):.4f}s p95={p95:.4f}s max={max(values):.4f}s")
PY
)

read_interface_counters() {
  awk -v wanted="$interface:" '$1 == wanted { print $2, $10 }' /proc/net/dev
}

start_at=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
read -r start_rx start_tx <<EOF
$(read_interface_counters)
EOF
if [ -z "$start_rx" ] || [ -z "$start_tx" ]; then
  echo "capture interface is missing from /proc/net/dev: $interface" >&2
  exit 1
fi
traffic_until=$(( $(date +%s) + accuracy_duration ))
dd if=/dev/zero of="$tmp_dir/upload.bin" bs=1048576 count=1 status=none
(
  while [ "$(date +%s)" -lt "$traffic_until" ]; do
    curl --max-time 15 -fsS -o /dev/null 'https://speed.cloudflare.com/__down?bytes=1048576' || true
    curl --max-time 15 -fsS -o /dev/null --data-binary @"$tmp_dir/upload.bin" 'https://speed.cloudflare.com/__up' || true
    sleep 1
  done
) &
traffic_pid=$!

restart_after=$((accuracy_duration / 2))
[ "$restart_after" -ge 1 ] || restart_after=1
sleep "$restart_after"
systemctl restart flowlens-server
systemctl restart flowlens-agent
deadline=$(( $(date +%s) + 60 ))
until curl -fsS "$local_url/healthz" >/dev/null 2>&1; do
  [ "$(date +%s)" -lt "$deadline" ] || { echo "server did not recover after restart" >&2; exit 1; }
  sleep 1
done
wait "$traffic_pid"
traffic_pid=
end_at=$(date -u +%Y-%m-%dT%H:%M:%S.%NZ)
read -r end_rx end_tx <<EOF
$(read_interface_counters)
EOF
if [ -z "$end_rx" ] || [ -z "$end_tx" ]; then
  echo "capture interface disappeared during accuracy test" >&2
  exit 1
fi
os_inbound=$((end_rx - start_rx))
os_outbound=$((end_tx - start_tx))
sleep 8

database_totals=$(postgres_query "
  SELECT coalesce(sum(bytes) FILTER (WHERE direction='inbound'),0)::bigint || ' ' ||
         coalesce(sum(bytes) FILTER (WHERE direction='outbound'),0)::bigint
  FROM interface_deltas
  WHERE node_id='$node_id' AND interface='$interface'
    AND observed_at >= '$start_at'::timestamptz AND observed_at <= '$end_at'::timestamptz;")
read -r db_inbound db_outbound <<EOF
$database_totals
EOF
if [ -z "$db_inbound" ] || [ -z "$db_outbound" ]; then
  echo "database traffic totals are incomplete" >&2
  exit 1
fi
accuracy_error=$(python3 - "$os_inbound" "$os_outbound" "$db_inbound" "$db_outbound" "$accuracy_limit" <<'PY'
import sys
os_in, os_out, db_in, db_out = map(int, sys.argv[1:5])
limit=float(sys.argv[5])
if min(os_in, os_out) <= 0:
    raise SystemExit("operating-system counters did not advance")
in_error=abs(db_in-os_in)/os_in*100
out_error=abs(db_out-os_out)/os_out*100
if max(in_error, out_error) > limit:
    raise SystemExit(
        f"traffic accuracy exceeds {limit:g}%: "
        f"inbound={in_error:.3f}% (os={os_in}, db={db_in}) "
        f"outbound={out_error:.3f}% (os={os_out}, db={db_out})"
    )
print(f"inbound={in_error:.3f}% outbound={out_error:.3f}%")
PY
)
max_observation_gap=$(postgres_query "
  SELECT coalesce(max(extract(epoch FROM observed_at-previous_at)),0)
  FROM (
    SELECT observed_at,lag(observed_at) OVER (ORDER BY observed_at) previous_at
    FROM interface_deltas
    WHERE node_id='$node_id' AND interface='$interface'
      AND observed_at >= '$start_at'::timestamptz AND observed_at <= '$end_at'::timestamptz
  ) observations;")

connections_ready=$tmp_dir/connections_ready
FLOWLENS_ACCEPTANCE_LOCAL_URL=$local_url python3 - "$concurrent_connections" "$connections_ready" <<'PY' &
import concurrent.futures, os, socket, sys, time, urllib.parse
count=int(sys.argv[1])
ready=sys.argv[2]
target=urllib.parse.urlsplit(os.environ["FLOWLENS_ACCEPTANCE_LOCAL_URL"])
if target.scheme != "http" or not target.hostname:
    raise SystemExit("FLOWLENS_LOCAL_URL must be an HTTP URL with a host")
address=(target.hostname, target.port or 80)
def connect(_):
    sock=socket.create_connection(address, timeout=5)
    sock.sendall(b"GET /healthz HTTP/1.1\r\nHost: localhost\r\nConnection: keep-alive\r\n\r\n")
    return sock
with concurrent.futures.ThreadPoolExecutor(max_workers=100) as executor:
    sockets=list(executor.map(connect, range(count)))
with open(ready, "w", encoding="ascii") as output:
    output.write(str(len(sockets)))
time.sleep(20)
for sock in sockets:
    sock.close()
PY
load_pid=$!
deadline=$(( $(date +%s) + 20 ))
while [ ! -s "$connections_ready" ]; do
  kill -0 "$load_pid" 2>/dev/null || { wait "$load_pid"; echo "connection load generator failed" >&2; exit 1; }
  [ "$(date +%s)" -lt "$deadline" ] || { echo "connection load generator did not become ready" >&2; exit 1; }
  sleep 1
done
opened_connections=$(cat "$connections_ready")
[ "$opened_connections" -ge "$concurrent_connections" ] || { echo "opened only $opened_connections connections" >&2; exit 1; }
under_load_latency=$(curl -fsS --cookie "$cookie_file" -o /dev/null -w '%{time_total}' \
  "$public_url/api/v1/overview?node=$node_id&range=30d")
python3 - "$under_load_latency" <<'PY'
import sys
if float(sys.argv[1]) > 2:
    raise SystemExit("query exceeded two seconds under connection load")
PY
wait "$load_pid"
load_pid=

server_memory=$(systemctl show flowlens-server -p MemoryCurrent --value)
agent_memory=$(systemctl show flowlens-agent -p MemoryCurrent --value)
memory_total=$((server_memory + agent_memory))
[ "$memory_total" -le 734003200 ] || { echo "FlowLens memory exceeds 700 MiB: $memory_total" >&2; exit 1; }
server_memory_max=$(systemctl show flowlens-server -p MemoryMax --value)
agent_memory_max=$(systemctl show flowlens-agent -p MemoryMax --value)

attribution_evidence=$(FLOWLENS_URL=$public_url FLOWLENS_NODE_ID=$node_id FLOWLENS_COOKIE_FILE=$cookie_file \
  "$repo_root/scripts/verify-attribution.sh")
echo "$attribution_evidence"

if [ "$run_browser" = 1 ]; then
  (
    cd "$repo_root/web"
    FLOWLENS_E2E_URL=$public_url FLOWLENS_E2E_USERNAME=$admin_username \
      FLOWLENS_E2E_PASSWORD="$(cat "$password_file")" npx playwright test
  )
  browser_evidence="login and seven dashboard routes passed at 1440x900, 1024x768, 390x844 and 360x800"
else
  browser_evidence="not run (FLOWLENS_ACCEPTANCE_BROWSER=$run_browser)"
fi

commit=$(git -C "$repo_root" rev-parse HEAD)
server_checksum=$(sha256sum /usr/local/bin/flowlens-server | awk '{print $1}')
agent_checksum=$(sha256sum /usr/local/bin/flowlens-agent | awk '{print $1}')
server_status=$(systemctl show flowlens-server -p ActiveState -p SubState --value | paste -sd/ -)
agent_status=$(systemctl show flowlens-agent -p ActiveState -p SubState --value | paste -sd/ -)
node_status=$(python3 - "$nodes_file" "$node_id" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    nodes=json.load(source).get("items", [])
node=next(item for item in nodes if item.get("id") == sys.argv[2])
print(node.get("status", "unknown"))
PY
)
collector_health=$(python3 - "$health_file" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as source:
    data=json.load(source)
print(data.get("status", "unknown"))
PY
)
webhook_attempts=$(postgres_query 'SELECT count(*) FROM webhook_deliveries;')
generated_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)

umask 077
cat >"$report" <<EOF
# FlowLens Release Acceptance Report

Generated: $generated_at
Commit: \`$commit\`

| Check | Evidence |
| --- | --- |
| Public health | HTTPS login and authenticated session passed; unauthenticated API returned 401 |
| Integration fixtures | $integration_evidence |
| Server | systemd \`$server_status\`; binary SHA-256 \`$server_checksum\` |
| Agent | systemd \`$agent_status\`; binary SHA-256 \`$agent_checksum\` |
| Node | \`$node_id\` status \`$node_status\` |
| Collector health | \`$collector_health\` |
| Ingestion lag | ${ingestion_lag}s (limit 5s) |
| 30-day query latency | $query_latency (limit 2s p95) |
| Million-row query plan | $query_plan_evidence (limit 2s; requested-month partitions only) |
| Traffic accuracy | OS inbound $os_inbound bytes / database $db_inbound bytes; OS outbound $os_outbound bytes / database $db_outbound bytes; $accuracy_error (limit ${accuracy_limit}%) |
| Restart behavior | Agent and server restarted during the ${accuracy_duration}s run; maximum observed sample gap ${max_observation_gap}s |
| Concurrent connections | $opened_connections held open; 30-day query completed in ${under_load_latency}s |
| FlowLens RSS | $memory_total bytes (limit 734003200) |
| Server memory maximum | $server_memory_max bytes |
| Agent memory maximum | $agent_memory_max bytes |
| Webhook attempts | $webhook_attempts persisted production delivery rows; retry behavior passed in disposable integration fixtures |
| Controlled attribution | $attribution_evidence |
| Browser acceptance | $browser_evidence |

Screenshots are stored under \`web/test-results/\`. No passwords, tokens, cookies, or public IP addresses are included in this report.
EOF

echo "acceptance evidence written to $report"
