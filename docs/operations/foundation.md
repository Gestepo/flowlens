# FlowLens Native Operations

FlowLens has three independently operated parts: an existing external PostgreSQL database, the native `flowlens-server.service`, and the native `flowlens-agent.service`. An HTTPS reverse proxy publishes the server; it is not a FlowLens runtime dependency.

## Files and Ownership

- Server binary: `/usr/local/bin/flowlens-server`
- Agent binary: `/usr/local/bin/flowlens-agent`
- Frontend: `/opt/flowlens/web`
- Root-readable configuration: `/etc/flowlens`
- Server state and GeoIP data: `/var/lib/flowlens`
- Agent spool and checkpoints: `/var/lib/flowlens-agent`

PostgreSQL remains owned and backed up by the database administrator. FlowLens uses only its dedicated database and role.

## Routine Checks

Check native service and health state with:

```sh
systemctl is-active flowlens-server.service flowlens-agent.service
curl -fsS http://127.0.0.1:8088/healthz
journalctl -u flowlens-server.service -u flowlens-agent.service --since today
```

Then verify `https://monitor.example.com`, node `flowlens-node-1`, data freshness, and each collector in the dashboard. A healthy server does not by itself prove that the Agent or every optional collector is healthy.

## Operational Boundaries

Back up PostgreSQL before FlowLens or database maintenance. Keep the Agent spool on persistent local storage. Treat Docker socket access as privileged and disable it with `FLOWLENS_DOCKER_ATTRIBUTION=disabled` when container attribution is unnecessary. Treat Nginx Proxy Manager logs as read-only input and expose only the required log directory to the Agent's systemd namespace.

See [install.md](install.md), [upgrade.md](upgrade.md), [backup-restore.md](backup-restore.md), and [remove.md](remove.md) for lifecycle procedures.
