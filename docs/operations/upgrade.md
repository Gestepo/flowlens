# Upgrade and Roll Back

## Upgrade

1. Read the release notes and confirm the supported PostgreSQL and Agent versions.
2. Create and verify a backup with `scripts/backup.sh`.
3. Run `make build` from the release being installed.
4. Run `sudo make install-server`. The installer stages the native binary and frontend before restarting `flowlens-server.service`.
5. Confirm `systemctl is-active flowlens-server.service` and `curl -fsS http://127.0.0.1:8088/healthz`.
6. Log in at `https://monitor.example.com` and inspect node and collector freshness.
7. Reinstall the Agent with `sudo scripts/install-agent.sh bin/flowlens-agent deploy/agent.env` when the release notes require an Agent update.

Database migrations are idempotent and run before the HTTP server starts. The Agent retains its bounded spool while the server is unavailable and resumes its persisted interface-counter checkpoint after restart.

## Roll Back

Use a release-supported downgrade when one is documented. Otherwise treat rollback as a database restore:

1. Stop `flowlens-agent.service` and `flowlens-server.service`.
2. Restore the backup that matches the earlier release using [backup-restore.md](backup-restore.md).
3. Install the matching server, frontend, and Agent artifacts.
4. Verify both systemd units, native `/healthz`, public HTTPS, login, and collector freshness.

Never run an older server against a database after an incompatible migration. Keep the failed release artifacts and logs until the rollback has been reviewed.
