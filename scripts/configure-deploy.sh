#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -- "$(dirname "$0")" && pwd)
project_dir=$(dirname "$script_dir")
output_dir=${1:-"$project_dir/deploy"}
server_environment="$output_dir/server.env"
agent_environment="$output_dir/agent.env"
legacy_environment="$output_dir/server."
legacy_environment="${legacy_environment}compose.env"

server_environment_is_legacy() {
  grep -q '@postgresql:5432' "$server_environment" ||
    grep -q '^FLOWLENS_LISTEN_ADDRESS=0\.0\.0\.0:8088$' "$server_environment"
}

prepare_native_environment() {
  source_environment=$1
  target_environment=$2
  cp "$source_environment" "$target_environment"
  sed -i \
    -e 's/@postgresql:5432/@127.0.0.1:5432/g' \
    -e 's/^FLOWLENS_LISTEN_ADDRESS=0\.0\.0\.0:8088$/FLOWLENS_LISTEN_ADDRESS=127.0.0.1:8088/' \
    "$target_environment"
  chmod 0600 "$target_environment"
}

if [ -f "$server_environment" ] && [ -f "$agent_environment" ]; then
  temporary_environment=$(mktemp "$output_dir/.server.env.XXXXXX")
  temporary_legacy=""
  trap 'rm -f "$temporary_environment" "$temporary_legacy"' 0 HUP INT TERM
  prepare_native_environment "$server_environment" "$temporary_environment"

  if [ -e "$legacy_environment" ] || [ -L "$legacy_environment" ]; then
    if [ -L "$legacy_environment" ] ||
      [ ! -f "$legacy_environment" ] ||
      [ "$(stat -c '%a' "$legacy_environment")" != 600 ]
    then
      echo "refusing to remove untrusted legacy environment" >&2
      exit 1
    fi
    temporary_legacy=$(mktemp "$output_dir/.legacy.env.XXXXXX")
    prepare_native_environment "$legacy_environment" "$temporary_legacy"
    if ! cmp -s "$temporary_environment" "$temporary_legacy"; then
      echo "refusing to remove mismatched legacy environment" >&2
      exit 1
    fi
    if server_environment_is_legacy; then
      mv -f "$temporary_environment" "$server_environment"
      temporary_environment=""
    fi
    rm -f "$legacy_environment" "$temporary_legacy"
    temporary_legacy=""
    trap - 0 HUP INT TERM
    exit 0
  fi

  if server_environment_is_legacy; then
    mv -f "$temporary_environment" "$server_environment"
    temporary_environment=""
    trap - 0 HUP INT TERM
    exit 0
  fi
  rm -f "$temporary_environment"
  temporary_environment=""
  trap - 0 HUP INT TERM
fi

if [ -e "$legacy_environment" ] || [ -L "$legacy_environment" ]; then
  echo "refusing to create native configuration beside an orphaned legacy environment" >&2
  exit 1
fi

for name in server.env agent.env; do
  if [ -e "$output_dir/$name" ]; then
    echo "refusing to overwrite $output_dir/$name" >&2
    exit 1
  fi
done

mkdir -p "$output_dir"
database_password=$(openssl rand -hex 24)
agent_token=$(openssl rand -hex 32)
bootstrap_token=$(openssl rand -hex 32)
secret_key=$(openssl rand -hex 32)

cp "$project_dir/deploy/server.env.example" "$output_dir/server.env"
cp "$project_dir/deploy/agent.env.example" "$output_dir/agent.env"

sed -i "s/CHANGE_DB_PASSWORD/$database_password/g" "$output_dir/server.env"
sed -i "s/CHANGE_AGENT_TOKEN/$agent_token/g" "$output_dir/server.env" "$output_dir/agent.env"
sed -i "s/CHANGE_BOOTSTRAP_TOKEN/$bootstrap_token/g" "$output_dir/server.env"
sed -i "s/CHANGE_SECRET_KEY/$secret_key/g" "$output_dir/server.env"
chmod 0600 "$output_dir/server.env" "$output_dir/agent.env"
