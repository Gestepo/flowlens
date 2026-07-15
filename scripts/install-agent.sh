#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
  echo "run as root" >&2
  exit 1
fi

binary=${1:-./bin/flowlens-agent}
environment=${2:-./deploy/agent.env}

if [ ! -x "$binary" ]; then
  echo "agent binary is missing or not executable: $binary" >&2
  exit 1
fi
if [ ! -f "$environment" ]; then
  echo "agent environment file is missing: $environment" >&2
  exit 1
fi

if ! getent passwd flowlens-agent >/dev/null 2>&1; then
  useradd --system --home-dir /var/lib/flowlens-agent --shell /usr/sbin/nologin flowlens-agent
fi
docker_attribution=$(sed -n 's/^FLOWLENS_DOCKER_ATTRIBUTION=//p' "$environment" | tail -n 1)
case "${docker_attribution:-enabled}" in
  enabled)
    if getent group docker >/dev/null 2>&1; then usermod -a -G docker flowlens-agent; fi
    ;;
  disabled)
    if getent group docker >/dev/null 2>&1 && id -nG flowlens-agent | tr ' ' '\n' | grep -qx docker; then
      gpasswd -d flowlens-agent docker >/dev/null
    fi
    ;;
  *)
    echo "FLOWLENS_DOCKER_ATTRIBUTION must be enabled or disabled" >&2
    exit 1
    ;;
esac
install -d -m 0750 -o flowlens-agent -g flowlens-agent /var/lib/flowlens-agent /var/lib/flowlens-agent/spool
install -d -m 0755 /etc/flowlens
install -m 0755 "$binary" /usr/local/bin/flowlens-agent
install -m 0600 -o root -g root "$environment" /etc/flowlens/agent.env
install -m 0644 ./deploy/flowlens-agent.service /etc/systemd/system/flowlens-agent.service
install -m 0644 ./deploy/60-flowlens-perf.conf /etc/sysctl.d/60-flowlens-perf.conf
sysctl -q -p /etc/sysctl.d/60-flowlens-perf.conf
systemctl daemon-reload
systemctl enable flowlens-agent.service
systemctl restart flowlens-agent.service
