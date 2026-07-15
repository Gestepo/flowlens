#!/bin/sh
set -eu

dry_run=false
if [ "${1:-}" = "--dry-run" ]; then
  dry_run=true
elif [ "$#" -ne 0 ]; then
  echo "usage: $0 [--dry-run]" >&2
  exit 2
fi
test_root=${FLOWLENS_UNINSTALL_ROOT:-}
systemctl=${FLOWLENS_SYSTEMCTL:-systemctl}
if [ -z "$test_root" ] && [ "$(id -u)" -ne 0 ]; then
  echo "uninstall must run as root" >&2
  exit 1
fi
[ -z "$test_root" ] || [ -d "$test_root" ] || { echo "uninstall test root is missing" >&2; exit 1; }
command -v "$systemctl" >/dev/null 2>&1 || { echo "systemctl command is missing" >&2; exit 1; }

root_path() {
  printf '%s%s\n' "$test_root" "$1"
}

run() {
  if [ "$dry_run" = true ]; then
    printf 'would run:'
    for argument in "$@"; do printf ' %s' "$argument"; done
    printf '\n'
  else
    "$@"
  fi
}

failed=false
disable_unit() {
  unit=$1
  if [ "$dry_run" = true ]; then
    run "$systemctl" disable --now "$unit"
    return
  fi
  if ! load_state=$("$systemctl" show "$unit" -p LoadState --value 2>/dev/null); then
    echo "could not inspect $unit" >&2
    failed=true
    return
  fi
  case "$load_state" in
    not-found|'') return ;;
  esac
  if ! "$systemctl" disable --now "$unit"; then
    echo "could not disable $unit" >&2
    failed=true
  fi
}

disable_unit flowlens-agent.service
disable_unit flowlens-server.service
run rm -f "$(root_path /etc/systemd/system/flowlens-agent.service)" "$(root_path /etc/systemd/system/flowlens-server.service)"
run rm -f "$(root_path /usr/local/bin/flowlens-agent)" "$(root_path /usr/local/bin/flowlens-server)" "$(root_path /etc/sysctl.d/60-flowlens-perf.conf)"
run rm -rf "$(root_path /etc/flowlens)" "$(root_path /opt/flowlens/web)"
if ! run "$systemctl" daemon-reload; then
  echo "could not reload systemd" >&2
  failed=true
fi

printf '%s\n' "FlowLens application files removed. The PostgreSQL database, backups, and state directories were preserved."
[ "$failed" = false ]
