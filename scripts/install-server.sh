#!/bin/sh
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH

destination_root=${FLOWLENS_INSTALL_ROOT:-}
case "$destination_root" in
  "")
    ;;
  /*)
    destination_root=$(realpath -m -- "$destination_root")
    if [ "$destination_root" = / ]; then
      echo "FLOWLENS_INSTALL_ROOT must not be the filesystem root" >&2
      exit 1
    fi
    ;;
  *)
    echo "FLOWLENS_INSTALL_ROOT must be an absolute path" >&2
    exit 1
    ;;
esac

if [ -z "$destination_root" ] && [ "$(id -u)" -ne 0 ]; then
  echo "run as root" >&2
  exit 1
fi

test_host_action_log=${FLOWLENS_INSTALL_TEST_HOST_ACTION_LOG:-}
if [ -z "$destination_root" ] && [ -n "$test_host_action_log" ]; then
  echo "FLOWLENS_INSTALL_TEST_HOST_ACTION_LOG requires FLOWLENS_INSTALL_ROOT" >&2
  exit 1
fi

run_host_command() {
  if [ -n "$test_host_action_log" ]; then
    printf '%s\n' "$*" >> "$test_host_action_log"
    return 97
  fi
  "$@"
}

target_path() {
  path=$(realpath -m -- "$destination_root$1")
  if [ -n "$destination_root" ]; then
    case "$path" in
      "$destination_root"/*)
        ;;
      *)
        echo "installation destination escapes FLOWLENS_INSTALL_ROOT" >&2
        return 1
        ;;
    esac
  fi
  printf '%s\n' "$path"
}

script_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repository_dir=$(dirname "$script_dir")
binary=${1:-"$repository_dir/bin/flowlens-server"}
web_bundle=${2:-"$repository_dir/web/dist"}
environment=${3:-"$repository_dir/deploy/server.env"}
unit="$repository_dir/deploy/flowlens-server.service"
binary_destination=$(target_path /usr/local/bin/flowlens-server)
web_destination=$(target_path /opt/flowlens/web)
environment_destination=$(target_path /etc/flowlens/server.env)
state_destination=$(target_path /var/lib/flowlens)
geoip_destination=$(target_path /var/lib/flowlens/geoip)
unit_destination=$(target_path /etc/systemd/system/flowlens-server.service)

if [ -n "$destination_root" ]; then
  root_owner=$(id -u)
  root_group=$(id -g)
  state_owner=$root_owner
  state_group=$root_group
else
  root_owner=root
  root_group=root
  state_owner=flowlens
  state_group=flowlens
fi

case "${FLOWLENS_INSTALL_START:-true}" in
  true|false)
    install_start=${FLOWLENS_INSTALL_START:-true}
    ;;
  *)
    echo "FLOWLENS_INSTALL_START must be true or false" >&2
    exit 1
    ;;
esac

if [ ! -f "$binary" ] || [ ! -x "$binary" ]; then
  echo "server binary is missing or not executable: $binary" >&2
  exit 1
fi
if [ ! -d "$web_bundle" ] || [ ! -f "$web_bundle/index.html" ]; then
  echo "server web bundle is missing or incomplete: $web_bundle" >&2
  exit 1
fi
if [ ! -f "$environment" ]; then
  echo "server environment file is missing: $environment" >&2
  exit 1
fi
if [ ! -f "$unit" ]; then
  echo "server service unit is missing: $unit" >&2
  exit 1
fi

binary=$(realpath -- "$binary")
web_bundle=$(realpath -- "$web_bundle")
environment=$(realpath -- "$environment")
unit=$(realpath -- "$unit")

has_environment_value() {
  awk -v required="$1" '
    /^[[:space:]]*#/ { next }
    {
      line = $0
      sub(/^[[:space:]]*/, "", line)
      prefix = required "="
      if (index(line, prefix) == 1) {
        value = substr(line, length(prefix) + 1)
        sub(/^[[:space:]]*/, "", value)
        sub(/[[:space:]]*$/, "", value)
        last = value
        found = 1
      }
    }
    END {
      quote = substr(last, 1, 1)
      if (length(last) >= 2 && ((quote == "\"" && substr(last, length(last), 1) == "\"") ||
          (quote == sprintf("%c", 39) && substr(last, length(last), 1) == sprintf("%c", 39)))) {
        last = substr(last, 2, length(last) - 2)
      }
      exit found && last != "" && last !~ /CHANGE_[A-Z0-9_]+/ ? 0 : 1
    }
  ' "$environment"
}

validate_environment() {
  for key in \
    FLOWLENS_DATABASE_URL \
    FLOWLENS_AGENT_TOKEN \
    FLOWLENS_BOOTSTRAP_TOKEN \
    FLOWLENS_SECRET_KEY \
    FLOWLENS_PUBLIC_URL
  do
    if ! has_environment_value "$key"; then
      echo "server environment must set $key to a non-placeholder value" >&2
      exit 1
    fi
  done
}

validate_environment

staging_dir=$(mktemp -d /tmp/flowlens-install.XXXXXX)
trap 'rm -rf "$staging_dir"' 0 HUP INT TERM

install -m 0755 "$binary" "$staging_dir/flowlens-server"
install -d -m 0755 "$staging_dir/web"
cp -R "$web_bundle"/. "$staging_dir/web"/
install -m 0600 "$environment" "$staging_dir/server.env"
install -m 0644 "$unit" "$staging_dir/flowlens-server.service"

if [ -z "$destination_root" ]; then
  if ! run_host_command getent group flowlens >/dev/null 2>&1; then
    run_host_command groupadd --system flowlens
  fi
  if ! run_host_command getent passwd flowlens >/dev/null 2>&1; then
    run_host_command useradd --system --gid flowlens --home-dir /var/lib/flowlens --shell /usr/sbin/nologin flowlens
  fi
fi

install -d -m 0755 -o "$root_owner" -g "$root_group" \
  "$(target_path /usr/local/bin)" \
  "$(target_path /opt/flowlens)" \
  "$(target_path /etc/flowlens)" \
  "$(target_path /etc/systemd/system)"
install -d -m 0755 -o "$root_owner" -g "$root_group" "$web_destination"
find "$web_destination" -mindepth 1 -delete
cp -R "$staging_dir/web"/. "$web_destination"/
chown -R "$root_owner:$root_group" "$web_destination"
find "$web_destination" -type d -exec chmod 0755 {} +
find "$web_destination" -type f -exec chmod 0644 {} +
install -d -m 0750 -o "$state_owner" -g "$state_group" "$state_destination" "$geoip_destination"
install -m 0755 -o "$root_owner" -g "$root_group" "$staging_dir/flowlens-server" "$binary_destination"
install -m 0600 -o "$root_owner" -g "$root_group" "$staging_dir/server.env" "$environment_destination"
install -m 0644 -o "$root_owner" -g "$root_group" "$staging_dir/flowlens-server.service" "$unit_destination"

if [ -z "$destination_root" ]; then
  run_host_command systemctl daemon-reload
  run_host_command systemctl enable flowlens-server.service
  case "$install_start" in
    true)
      run_host_command systemctl restart flowlens-server.service
      ;;
    false)
      ;;
  esac
fi
