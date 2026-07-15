# Backup and Restore

## Backup

Use the installed root-readable server environment and choose a protected destination:

```sh
sudo env FLOWLENS_SERVER_ENVIRONMENT=/etc/flowlens/server.env \
  scripts/backup.sh /var/backups/flowlens
```

Each run creates a PostgreSQL custom-format dump, a generic manifest, and SHA-256 checksums with mode `0600`. The manifest records only the creation time, dump format, and dump-tool version; it does not copy configuration secrets.

Store the dump, manifest, and checksum together. Test restoration periodically against a disposable empty database by using a separate root-readable target environment file; a backup that has not been restored is not verified recovery evidence.

## Restore

Restore is destructive to the selected empty target and requires both native services to be stopped:

1. Stop writes with `sudo systemctl stop flowlens-agent.service flowlens-server.service`.
2. Create an empty target database owned by the existing dedicated `flowlens` role using separately authenticated PostgreSQL administration tooling. `FLOWLENS_POSTGRES_ADMIN_URL` is used only to create the empty target database; the restore script does not consume it.
3. Create `/etc/flowlens/restore-target.env` with mode `0600`. Set `FLOWLENS_DATABASE_URL` in that file to the target database using the application role, not the administrator role.
4. Restore using the root-readable target configuration:

   ```sh
   sudo env FLOWLENS_SERVER_ENVIRONMENT=/etc/flowlens/restore-target.env \
     scripts/restore.sh /var/backups/flowlens/flowlens-TIMESTAMP.dump
   ```

Avoid putting a database URL directly in a command because it can be retained in shell history or exposed through process inspection. Use `sudoedit` or another secret-aware configuration process to populate the target environment file, and remove that temporary file after the restore has been verified.

The restore script verifies the checksum, refuses a non-empty target, applies the archive in one transaction without changing shared PostgreSQL ownership, starts the server, checks `/healthz`, and starts the Agent only after the server is healthy. Finally verify both systemd units, `https://monitor.example.com/healthz`, administrator login, node freshness, and collector health.
