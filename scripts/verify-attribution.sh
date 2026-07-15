#!/bin/sh
set -eu

base_url=${FLOWLENS_URL:-https://monitor.example.com}
node_id=${FLOWLENS_NODE_ID:-flowlens-node-1}
public_domain=${FLOWLENS_DOMAIN:-${base_url#*://}}
public_domain=${public_domain%%/*}
public_domain=${public_domain%%:*}
cookie_file=${FLOWLENS_COOKIE_FILE:-}
docker_attribution=${FLOWLENS_DOCKER_ATTRIBUTION:-disabled}
container=
container_id=

for command in curl python3 systemctl; do
  command -v "$command" >/dev/null 2>&1 || { echo "required command is missing: $command" >&2; exit 1; }
done
case "$docker_attribution" in
  enabled)
    command -v docker >/dev/null 2>&1 || { echo "Docker attribution verification requires Docker" >&2; exit 1; }
    container="flowlens-e2e-client-$$"
    ;;
  disabled) ;;
  *) echo "FLOWLENS_DOCKER_ATTRIBUTION must be enabled or disabled" >&2; exit 1 ;;
esac

# shellcheck disable=SC2317
cleanup() {
  if [ -n "$container_id" ]; then
    docker rm -f "$container_id" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

run_started=$(date -u +%Y-%m-%dT%H:%M:00Z)
curl -fsS "$base_url/healthz" >/dev/null
if [ "$docker_attribution" = enabled ]; then
  if docker container inspect "$container" >/dev/null 2>&1; then
    echo "verification container already exists: $container" >&2
    exit 1
  fi
  container_id=$(docker run -d --rm --name "$container" --network host alpine:3.21 sh -c '
  apk add --no-cache ca-certificates >/dev/null
  i=0
  while [ "$i" -lt 55 ]; do
    nslookup example.com >/dev/null 2>&1 || true
    nslookup example.net >/dev/null 2>&1 || true
    wget -qO- https://example.com >/dev/null 2>&1 || true
    wget -qO- http://example.net >/dev/null 2>&1 || true
    printf x | nc -u -w 1 198.51.100.1 9 >/dev/null 2>&1 || true
    i=$((i + 1))
    sleep 1
  done
')
fi

api_curl() {
  if [ -n "$cookie_file" ]; then
    curl --cookie "$cookie_file" "$@"
  else
    curl "$@"
  fi
}

query() {
  endpoint=$1
  shift
  start=$run_started
  end=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  api_curl -fsSG "$base_url/api/v1/$endpoint" \
    --data-urlencode "node=$node_id" \
    --data-urlencode "start=$start" \
    --data-urlencode "end=$end" \
    --data-urlencode 'limit=100' \
    "$@"
}

has_item() {
  python3 -c 'import json,sys
criteria=dict(value.split("=", 1) for value in sys.argv[1:])
minimum=int(criteria.pop("bytes_min", "1"))
maximum=int(criteria.pop("bytes_max", str(2**63-1)))
items=json.load(sys.stdin).get("items", [])
ok=any(all(str(item.get(key, "")) == value for key, value in criteria.items()) and minimum <= item.get("bytes", 0) <= maximum for item in items)
raise SystemExit(0 if ok else 1)' "$@"
}

item_field() {
  field=$1
  shift
  python3 -c 'import json,sys
field=sys.argv[1]
criteria=dict(value.split("=", 1) for value in sys.argv[2:])
for item in json.load(sys.stdin).get("items", []):
    if all(str(item.get(key, "")) == value for key, value in criteria.items()):
        print(item.get(field, ""))
        raise SystemExit(0)
raise SystemExit(1)' "$field" "$@"
}

sum_field() {
  field=$1
  shift
  python3 -c 'import json,sys
field=sys.argv[1]
criteria=dict(value.split("=", 1) for value in sys.argv[2:])
items=json.load(sys.stdin).get("items", [])
print(sum(int(item.get(field, 0) or 0) for item in items if all(str(item.get(key, "")) == value for key, value in criteria.items())))' "$field" "$@"
}

is_fresh() {
  python3 -c 'import datetime,json,sys
value=json.load(sys.stdin).get("data_fresh_at", "")
fresh=datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
started=datetime.datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00"))
raise SystemExit(0 if fresh >= started else 1)' "$1"
}

server_pid=$(systemctl show flowlens-server.service -p MainPID --value)
case "$server_pid" in *[!0-9]*|''|0) echo "flowlens-server.service has no running process" >&2; exit 1;; esac
server_process=$(sed -n '1p' "/proc/$server_pid/comm")
[ -n "$server_process" ] || { echo "flowlens-server.service process name is unavailable" >&2; exit 1; }
expected_server_owner="process:$server_pid:$server_process"
baseline_inbound=$(query domains --data-urlencode 'direction=inbound')
baseline_inbound_requests=$(printf '%s' "$baseline_inbound" | item_field requests "domain=$public_domain" 'direction=inbound' 'confidence=confirmed' || printf '0')
baseline_flows=$(query flows --data-urlencode 'direction=inbound' --data-urlencode "domain=$public_domain")
baseline_flow_requests=$(printf '%s' "$baseline_flows" | sum_field requests "owner_id=$expected_server_owner" 'remote_port=8088' "domain=$public_domain" 'direction=inbound' 'confidence=confirmed')
generated_requests=6
request=0
while [ "$request" -lt "$generated_requests" ]; do
  curl -fsS "$base_url/" >/dev/null
  request=$((request + 1))
done

deadline=$(( $(date +%s) + 90 ))
server_owner_ok=false
client_owner_ok=true
tls_ok=true
dns_ok=true
ip_ok=true
if [ "$docker_attribution" = enabled ]; then
  client_owner_ok=false
  tls_ok=false
  dns_ok=false
  ip_ok=false
fi
inbound_ok=false
inbound_flow_ok=false
while [ "$(date +%s)" -lt "$deadline" ]; do
  owners=$(query owners || true)
  inbound=$(query domains --data-urlencode 'direction=inbound' || true)
  inbound_flows=$(query flows --data-urlencode 'direction=inbound' --data-urlencode "domain=$public_domain" || true)
  observed_server_owner=$(printf '%s' "$owners" | item_field id "id=$expected_server_owner" 'kind=process' "name=$server_process" || true)
  if [ "$observed_server_owner" = "$expected_server_owner" ]; then
    server_owner_ok=true
  fi
  if [ "$docker_attribution" = enabled ]; then
    owner_id=$(printf '%s' "$owners" | item_field id "name=$container" || true)
    if [ -n "$owner_id" ]; then
      client_owner_ok=true
      tls_flows=$(query flows --data-urlencode "owner=$owner_id" --data-urlencode 'domain=example.com' || true)
      dns_flows=$(query flows --data-urlencode "owner=$owner_id" --data-urlencode 'domain=example.net' || true)
      ip_flows=$(query flows --data-urlencode "owner=$owner_id" --data-urlencode 'domain=198.51.100.1' || true)
      if printf '%s' "$tls_flows" | has_item "owner_name=$container" 'direction=outbound' 'confidence=confirmed' 'bytes_min=1000' 'bytes_max=5000000'; then tls_ok=true; fi
      if printf '%s' "$dns_flows" | has_item "owner_name=$container" 'direction=outbound' 'confidence=inferred' 'bytes_min=1000' 'bytes_max=5000000'; then dns_ok=true; fi
      if printf '%s' "$ip_flows" | has_item "owner_name=$container" 'direction=outbound' 'confidence=ip_only' 'bytes_min=1' 'bytes_max=10000'; then ip_ok=true; fi
    fi
  fi
  inbound_requests=$(printf '%s' "$inbound" | item_field requests "domain=$public_domain" 'direction=inbound' 'confidence=confirmed' || true)
  if [ -n "$inbound_requests" ] && [ $((inbound_requests - baseline_inbound_requests)) -ge "$generated_requests" ]; then
    inbound_ok=true
  fi
  inbound_flow_requests=$(printf '%s' "$inbound_flows" | sum_field requests "owner_id=$expected_server_owner" 'remote_port=8088' "domain=$public_domain" 'direction=inbound' 'confidence=confirmed' || printf '0')
  inbound_flow_source=$(printf '%s' "$inbound_flows" | item_field source "owner_id=$expected_server_owner" 'remote_port=8088' "domain=$public_domain" 'direction=inbound' 'confidence=confirmed' || true)
  if [ -n "$inbound_flow_source" ] && [ $((inbound_flow_requests - baseline_flow_requests)) -ge "$generated_requests" ]; then inbound_flow_ok=true; fi
  printf '%s' "$owners" | is_fresh "$run_started" || { sleep 3; continue; }
  if [ "$server_owner_ok" = true ] && [ "$client_owner_ok" = true ] && [ "$tls_ok" = true ] && [ "$dns_ok" = true ] && [ "$ip_ok" = true ] && [ "$inbound_ok" = true ] && [ "$inbound_flow_ok" = true ]; then
    echo "attribution verification passed: server_owner=$expected_server_owner, client=${container:-disabled}, inbound process flow and configured outbound evidence are visible"
    exit 0
  fi
  sleep 3
done

echo "attribution verification failed: server_owner=$server_owner_ok client_owner=$client_owner_ok inbound=$inbound_ok inbound_flow=$inbound_flow_ok tls=$tls_ok dns=$dns_ok direct_ip=$ip_ok" >&2
exit 1
