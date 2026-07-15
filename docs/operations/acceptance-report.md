# FlowLens Release Acceptance Report - TEMPLATE

> No acceptance run has been recorded in this file. This is a blank template, not proof that a release was tested. Replace pending fields only with reviewed evidence from the release being evaluated.

Generated: Pending acceptance run
Release revision: Pending acceptance run
Environment: Pending acceptance run (use generic labels; do not record public IP addresses)

| Check | Acceptance threshold | Evidence |
| --- | --- | --- |
| Public health | HTTPS health, login, and authenticated session succeed; unauthenticated API is rejected | Pending acceptance run |
| Integration fixtures | Authentication, retention, traffic-query, and webhook suites pass against a disposable database | Pending acceptance run |
| Server service | Native systemd unit is active and the installed artifact is identified | Pending acceptance run |
| Agent service | Native systemd unit is active and the installed artifact is identified | Pending acceptance run |
| Node and collector health | Expected example node `flowlens-node-1` is fresh; degraded collectors are explained | Pending acceptance run |
| Ingestion lag | At or below the release threshold | Pending acceptance run |
| Query latency and plan | At or below the release threshold and limited to expected partitions | Pending acceptance run |
| Traffic accuracy | Within the documented error threshold for a controlled transfer | Pending acceptance run |
| Restart behavior | No duplicate batches; any observation gap is reported | Pending acceptance run |
| Concurrent connections | Target concurrency is sustained without failed health checks | Pending acceptance run |
| Memory limits | Combined native service memory remains within the release threshold | Pending acceptance run |
| Webhook delivery | Delivery and retry fixtures pass without contacting an unrelated endpoint | Pending acceptance run |
| Controlled attribution | Process, inbound domain, and enabled optional collectors meet their expected evidence level | Pending acceptance run |
| Browser acceptance | Required routes and viewports pass against `https://monitor.example.com` or an equivalent test origin | Pending acceptance run |

Acceptance output must not include passwords, tokens, cookies, database URLs, private keys, public IP addresses, production inventory counts, or unrelated host identifiers. Record timestamps, revisions, checksums, byte totals, counts, and latency measurements only in a deliberately executed release report, never in this repository template.
