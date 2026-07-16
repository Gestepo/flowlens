#!/bin/sh
set -eu

usage() {
	printf '%s\n' 'usage: install-agent-remote.sh --node-id NODE_ID --endpoint HTTPS_ENDPOINT [--interface INTERFACE]' >&2
	exit 2
}

fail() {
	printf '%s\n' "$1" >&2
	exit 1
}

if [ "$(id -u)" -ne 0 ]; then
	fail 'run this installer with sudo or as root'
fi

node_id=
endpoint=
interface=
while [ "$#" -gt 0 ]; do
	case "$1" in
		--node-id)
			[ "$#" -ge 2 ] || usage
			node_id=$2
			shift 2
			;;
		--endpoint)
			[ "$#" -ge 2 ] || usage
			endpoint=$2
			shift 2
			;;
		--interface)
			[ "$#" -ge 2 ] || usage
			interface=$2
			shift 2
			;;
		-h|--help)
			usage
			;;
		*)
			usage
			;;
	esac
done

case "$node_id" in
	''|*[!A-Za-z0-9_.:-]*) fail 'node ID must use only letters, digits, dot, underscore, colon, or hyphen' ;;
esac
[ "${#node_id}" -le 128 ] || fail 'node ID must be at most 128 characters'

case "$endpoint" in
	https://*/api/v1/agent/batches) ;;
	*) fail 'endpoint must be an HTTPS FlowLens batch endpoint' ;;
esac
authority=${endpoint#https://}
authority=${authority%%/*}
[ -n "$authority" ] || fail 'endpoint must include a hostname'
case "$endpoint" in
	*[[:space:]]*) fail 'endpoint cannot contain whitespace' ;;
esac

if [ -z "$interface" ]; then
	interface=$(ip route show default 2>/dev/null | awk '$1 == "default" { for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit } }')
fi
case "$interface" in
	''|*[!A-Za-z0-9_.:-]*) fail 'interface name is invalid; pass --interface explicitly' ;;
esac
[ -d "/sys/class/net/$interface" ] || fail "interface $interface does not exist"

[ -r /etc/os-release ] || fail 'this installer requires Debian or Ubuntu'
# shellcheck disable=SC1091
. /etc/os-release
case "${ID:-}" in
	debian|ubuntu) ;;
	*) fail 'this installer supports Debian and Ubuntu only' ;;
esac

case "$(uname -m)" in
	x86_64) go_arch=amd64 ;;
	aarch64) go_arch=arm64 ;;
	*) fail 'this installer supports amd64 and arm64 only' ;;
esac

if ! [ -r /dev/tty ] || ! [ -w /dev/tty ]; then
	fail 'a terminal is required to enter the Agent token privately'
fi
restore_terminal() {
	stty echo </dev/tty 2>/dev/null || true
}
trap restore_terminal 0 1 2 3 15
printf '%s' 'FlowLens Agent token (input hidden): ' >/dev/tty
stty -echo </dev/tty
if ! IFS= read -r agent_token </dev/tty; then
	agent_token=
fi
restore_terminal
printf '\n' >/dev/tty
[ -n "$agent_token" ] || fail 'Agent token cannot be empty'
case "$agent_token" in
	*"
"*) fail 'Agent token cannot contain a newline' ;;
esac

umask 077
workspace=$(mktemp -d /tmp/flowlens-agent-install.XXXXXX)
cleanup() {
	rm -rf "$workspace"
}
trap cleanup 0

apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y ca-certificates curl git build-essential clang llvm libelf-dev zlib1g-dev pkg-config iproute2

go_version=1.26.4
go_archive="$workspace/go${go_version}.linux-${go_arch}.tar.gz"
curl --fail --location --proto '=https' --tlsv1.2 "https://go.dev/dl/go${go_version}.linux-${go_arch}.tar.gz" -o "$go_archive"
tar -C "$workspace" -xzf "$go_archive"

source_dir="$workspace/source"
git clone --depth 1 https://github.com/Gestepo/flowlens.git "$source_dir"
(cd "$source_dir" && CGO_ENABLED=1 "$workspace/go/bin/go" build -o bin/flowlens-agent ./cmd/flowlens-agent)

environment="$workspace/agent.env"
{
	printf 'FLOWLENS_NODE_ID=%s\n' "$node_id"
	printf 'FLOWLENS_SERVER_ENDPOINT=%s\n' "$endpoint"
	printf 'FLOWLENS_AGENT_TOKEN=%s\n' "$agent_token"
	printf '%s\n' 'FLOWLENS_SPOOL_DIR=/var/lib/flowlens-agent/spool'
	printf '%s\n' 'FLOWLENS_INTERFACE_COUNTERS_PATH=/proc/net/dev'
	printf '%s\n' 'FLOWLENS_INTERVAL=2s'
	printf '%s\n' 'FLOWLENS_NPM_LOG_GLOBS='
	printf 'FLOWLENS_CAPTURE_INTERFACES=%s\n' "$interface"
	printf '%s\n' 'FLOWLENS_DOCKER_ATTRIBUTION=disabled'
} >"$environment"

(cd "$source_dir" && ./scripts/install-agent.sh "$source_dir/bin/flowlens-agent" "$environment")
if ! systemctl is-active --quiet flowlens-agent.service; then
	printf '%s\n' 'FlowLens Agent did not start. Inspect: journalctl -u flowlens-agent.service -n 100 --no-pager' >&2
	exit 1
fi

printf '%s\n' "FlowLens Agent is active for node $node_id."
