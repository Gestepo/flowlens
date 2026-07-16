#!/bin/sh
set -eu

if [ "$(id -u)" -ne 0 ]; then
	printf '%s\n' 'run this uninstaller with sudo or as root' >&2
	exit 1
fi
if [ "$#" -ne 1 ] || [ "$1" != '--yes' ]; then
	printf '%s\n' 'usage: uninstall-agent-remote.sh --yes' >&2
	exit 2
fi

systemctl disable --now flowlens-agent.service >/dev/null 2>&1 || true
rm -f /etc/systemd/system/flowlens-agent.service
rm -f /usr/local/bin/flowlens-agent
rm -f /etc/flowlens/agent.env
rm -f /etc/sysctl.d/60-flowlens-perf.conf
rm -rf /var/lib/flowlens-agent

if getent passwd flowlens-agent >/dev/null 2>&1; then
	userdel flowlens-agent || true
fi
if getent group flowlens-agent >/dev/null 2>&1; then
	groupdel flowlens-agent || true
fi

systemctl daemon-reload
sysctl --system >/dev/null 2>&1 || true
printf '%s\n' 'FlowLens Agent has been removed from this VPS.'
