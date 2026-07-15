#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
project_dir=$(dirname "$script_dir")
environment=${FLOWLENS_SERVER_ENV:-"$project_dir/deploy/server.env"}
endpoint=${FLOWLENS_BOOTSTRAP_URL:-http://127.0.0.1:8088/api/v1/auth/bootstrap}
username=${FLOWLENS_ADMIN_USERNAME:-admin}

test -r "$environment" || { echo "server environment file is not readable" >&2; exit 1; }
token=$(awk -F= '$1 == "FLOWLENS_BOOTSTRAP_TOKEN" { print substr($0,index($0,"=")+1); exit }' "$environment")
test -n "$token" || { echo "bootstrap token is missing" >&2; exit 1; }

printf 'Administrator username [%s]: ' "$username"
trap 'stty echo 2>/dev/null || true' EXIT HUP INT TERM
read -r entered_username
if [ -n "$entered_username" ]; then username=$entered_username; fi
printf 'Administrator password: '
stty -echo
read -r password
stty echo
printf '\nConfirm password: '
stty -echo
read -r confirmation
stty echo
printf '\n'

test "$password" = "$confirmation" || { echo "passwords do not match" >&2; exit 1; }
test "${#password}" -ge 12 || { echo "password must contain at least 12 characters" >&2; exit 1; }
payload=$(printf '%s\n%s' "$username" "$password" | python3 -c 'import json,sys; print(json.dumps({"username":sys.stdin.readline().rstrip("\n"),"password":sys.stdin.read()}))')
curl -fsS -X POST -H "Authorization: Bearer $token" -H 'Content-Type: application/json' --data-binary "$payload" "$endpoint" >/dev/null
printf '%s\n' "administrator created"
