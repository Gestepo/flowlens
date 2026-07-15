# Detailed Traffic Attribution Operations

## Collectors

The native Agent combines eBPF and host socket byte deltas, optional Docker inventory, optional Nginx Proxy Manager access logs, and bounded DNS/TLS name evidence. One degraded collector is reported as partial data without hiding results from the others.

Set `FLOWLENS_CAPTURE_INTERFACES` to the real public interface; do not copy an interface name from another host. The generic NPM glob `/data/logs/proxy-host-*_access.log` refers to the Agent's systemd namespace. The base unit contains no host-specific bind. To enable NPM log attribution, run `sudo systemctl edit flowlens-agent.service` and add a drop-in like this, replacing the source placeholder with the actual host log directory:

```ini
[Service]
BindReadOnlyPaths=/path/to/npm/logs:/data/logs
```

Run `sudo systemctl daemon-reload` and restart `flowlens-agent.service`. If NPM log attribution is not used, leave `FLOWLENS_NPM_LOG_GLOBS` empty and do not add the drop-in.

Native process attribution also requires the upstream port. Configure NPM's `proxy` access-log format to end its upstream field with `[Sent-to $server:$port]`, rather than recording `$server` alone. For NPM installations that expose `log.conf`, the complete compatible format is:

```nginx
log_format proxy '[$time_local] $upstream_cache_status $upstream_status $status - $request_method $scheme $host "$request_uri" [Client $remote_addr] [Length $body_bytes_sent] [Gzip $gzip_ratio] [Sent-to $server:$port] "$http_user_agent" "$http_referer"';
```

Persist this file using the reverse proxy's normal configuration mechanism, reload NPM, and confirm a new access-log line contains both the upstream address and port. Existing host-only records remain visible as historical upstream traffic but cannot be mapped safely to a native process when several services share one host address.

The packaged performance sysctl permits the dedicated Agent to attach supported probes without `CAP_SYS_ADMIN`. Review this host-wide setting against local hardening policy. Docker attribution is disabled by default and separately controlled by `FLOWLENS_DOCKER_ATTRIBUTION`; explicitly enabling it grants effectively host-level Docker socket access.

## Verification

After installing both native services, export the real public URL and stable node ID:

```sh
sudo FLOWLENS_URL=https://monitor.example.com \
  FLOWLENS_NODE_ID=flowlens-node-1 \
  FLOWLENS_COOKIE_FILE=/path/to/admin-cookie.txt \
  scripts/verify-attribution.sh
```

The verifier checks systemd process ownership and public inbound Host attribution. With `FLOWLENS_DOCKER_ATTRIBUTION=enabled`, it also creates one uniquely named temporary client container for controlled outbound evidence and removes only the container ID it created. Docker is not used to run FlowLens.

Run authenticated browser checks against the same public origin:

```sh
cd web
FLOWLENS_E2E_URL=https://monitor.example.com npx playwright test
```

## Evidence Levels

- `confirmed`: an inbound proxy Host or outbound TLS SNI directly matched the connection.
- `inferred`: recent DNS evidence matched the destination IP; shared hosting can make the name ambiguous.
- `ip_only`: no defensible domain evidence was available, so the dashboard keeps the destination as an IP.

FlowLens never stores packet payloads, command-line arguments, URL query values, cookies, or DNS packet bodies.
