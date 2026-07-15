# Remove FlowLens

Preview the native removal commands:

```sh
sudo scripts/uninstall.sh --dry-run
```

Then remove the systemd units, binaries, configuration, and frontend:

```sh
sudo scripts/uninstall.sh
```

The script disables `flowlens-agent.service` and `flowlens-server.service` before removing application files. It deliberately preserves:

- the external PostgreSQL instance, `flowlens` database, and database role;
- `/var/lib/flowlens` and `/var/lib/flowlens-agent` state;
- backup files created outside the application directories.

Confirm both units are absent with `systemctl status flowlens-server.service flowlens-agent.service`. Remove the preserved database, role, state, Docker group membership, or reverse-proxy host only through separate reviewed administration steps. Deleting any of them is irreversible and is never performed by the uninstall script.
