# Install FlowLens

FlowLens runs as two native systemd services. Docker is not required to run the server or Agent; it is used only when optional container attribution is enabled.

## Prerequisites

- A supported Linux host with systemd, a C toolchain, Go, Node.js, npm, curl, OpenSSL, Python 3, and PostgreSQL client tools.
- An existing PostgreSQL instance reachable from the FlowLens host. Create a dedicated `flowlens` role and database; do not reuse another application's role or schema.
- An HTTPS reverse proxy and DNS for a public name such as `monitor.example.com`.
- Root access for installing the native services. The database administrator can remain a separate operator.

FlowLens treats 10 GiB as its default PostgreSQL data budget and warns at 80%. Change `FLOWLENS_DATABASE_BUDGET_BYTES` when the planned budget differs.

## Prepare Configuration

1. Run `scripts/configure-deploy.sh` once. It copies the examples to `deploy/server.env` and `deploy/agent.env`, generates secrets, and refuses to overwrite existing files.
2. Replace `CHANGE_CAPTURE_INTERFACE` in `deploy/agent.env` with the host interface reported by `ip route show default`, and keep the public example node ID `flowlens-node-1` or choose a stable local ID.
3. Set `FLOWLENS_PUBLIC_URL=https://monitor.example.com` in `deploy/server.env` to the real HTTPS origin.
4. Create the PostgreSQL role and empty database. Set the role password to the generated password in `FLOWLENS_DATABASE_URL`; enable the SSL mode required by the external PostgreSQL server.

The example database address uses loopback only as a native single-host default. Use the PostgreSQL server's actual private or Unix-socket address when it runs elsewhere.

## Install the Server

1. Run `make build`.
2. Run `sudo make install-server`. The installer adds the `flowlens` system user and installs the binary, frontend, environment, and systemd unit.
3. Verify `systemctl is-active flowlens-server.service` reports `active`.
4. Verify native health with `curl -fsS http://127.0.0.1:8088/healthz`.
5. Run `scripts/bootstrap-admin.sh` and set the administrator password. Bootstrap succeeds only once.

Inspect failures with `journalctl -u flowlens-server.service`.

## Install the Agent

1. Build the Agent with `CGO_ENABLED=1 go build -o bin/flowlens-agent ./cmd/flowlens-agent`.
2. Run `sudo scripts/install-agent.sh bin/flowlens-agent deploy/agent.env`.
3. Verify `systemctl is-active flowlens-agent.service` reports `active`, then inspect collector state in the dashboard.

`FLOWLENS_NPM_LOG_GLOBS=/data/logs/proxy-host-*_access.log` is a generic path inside the Agent's systemd namespace. The packaged unit does not assume where Nginx Proxy Manager stores logs. Add a `BindReadOnlyPaths` systemd drop-in that maps the actual host log directory to `/data/logs`, then restart the Agent. [attribution.md](attribution.md) shows the drop-in. Leave the glob empty when NPM log attribution is not used.

Docker attribution is disabled by default. Enable `FLOWLENS_DOCKER_ATTRIBUTION=enabled` only when container ownership is required and the privilege has been reviewed. Enabling it grants the Agent Docker socket access; Docker group membership is effectively host-level privilege even though FlowLens performs inventory reads only.

## Configure HTTPS

For native Nginx, Caddy, or another host reverse proxy, send `monitor.example.com` to `http://127.0.0.1:8088`, preserve the `Host` header, and permit WebSocket upgrades. Confirm HTTPS and login before enabling HSTS.

For a containerized Nginx Proxy Manager, add a generic host mapping such as `host.docker.internal:host-gateway` to the proxy container. The host gateway cannot reach a service bound only to host loopback, so bind FlowLens to a firewall-restricted host address reachable from that bridge and use `host.docker.internal:8088` as the NPM upstream. For example documentation, `192.0.2.10` is an RFC-reserved address; substitute the real private host address. Do not deploy FlowLens itself in the reverse-proxy container stack.

Do not add an NPM Access List to the FlowLens host unless it is intentionally part of the deployment; FlowLens provides its own administrator session. Verify `https://monitor.example.com/healthz`, log in, and then run the attribution checks described in [attribution.md](attribution.md).

Release acceptance creates and removes a disposable integration database. Set `FLOWLENS_POSTGRES_ADMIN_URL` to a separate administrative connection; never use the application `FLOWLENS_DATABASE_URL` for database creation or removal.
