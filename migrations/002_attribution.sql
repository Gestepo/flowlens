CREATE TABLE IF NOT EXISTS owners (
  node_id text NOT NULL REFERENCES nodes(id),
  owner_id text NOT NULL,
  kind text NOT NULL CHECK (kind IN ('host', 'process', 'container')),
  display_name text NOT NULL,
  pid integer,
  container_id text,
  cgroup_id bigint,
  addresses jsonb NOT NULL DEFAULT '[]'::jsonb,
  ports jsonb NOT NULL DEFAULT '[]'::jsonb,
  running boolean NOT NULL DEFAULT true,
  first_seen_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  PRIMARY KEY (node_id, owner_id)
);

CREATE INDEX IF NOT EXISTS owners_node_running_idx
  ON owners (node_id, running, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS domain_evidence (
  event_id text PRIMARY KEY,
  node_id text NOT NULL REFERENCES nodes(id),
  observed_at timestamptz NOT NULL,
  ip inet NOT NULL,
  name text NOT NULL,
  source text NOT NULL CHECK (source IN ('dns', 'tls_sni')),
  valid_from timestamptz NOT NULL,
  valid_until timestamptz NOT NULL CHECK (valid_until > valid_from)
);

CREATE INDEX IF NOT EXISTS domain_evidence_node_ip_valid_idx
  ON domain_evidence (node_id, ip, valid_from DESC, valid_until);

CREATE TABLE IF NOT EXISTS connection_details (
  event_id text NOT NULL,
  node_id text NOT NULL REFERENCES nodes(id),
  observed_at timestamptz NOT NULL,
  direction text NOT NULL CHECK (direction IN ('inbound', 'outbound', 'internal', 'container_to_container')),
  protocol text NOT NULL,
  local_ip inet NOT NULL,
  local_port integer NOT NULL CHECK (local_port BETWEEN 1 AND 65535),
  remote_ip inet NOT NULL,
  remote_port integer NOT NULL CHECK (remote_port BETWEEN 1 AND 65535),
  owner_id text NOT NULL,
  owner_kind text NOT NULL CHECK (owner_kind IN ('host', 'process', 'container')),
  owner_name text NOT NULL,
  display_name text NOT NULL,
  confidence text NOT NULL CHECK (confidence IN ('confirmed', 'inferred', 'ip_only')),
  evidence_source text NOT NULL DEFAULT '',
  evidence_observed_at timestamptz,
  bytes_sent bigint NOT NULL CHECK (bytes_sent >= 0),
  bytes_received bigint NOT NULL CHECK (bytes_received >= 0),
  state text NOT NULL DEFAULT '',
  country_code text NOT NULL DEFAULT '',
  country_name text NOT NULL DEFAULT '',
  asn bigint NOT NULL DEFAULT 0,
  organization text NOT NULL DEFAULT '',
  network_classification text NOT NULL DEFAULT 'unknown',
  PRIMARY KEY (event_id, observed_at)
) PARTITION BY RANGE (observed_at);

CREATE TABLE IF NOT EXISTS connection_details_default
  PARTITION OF connection_details DEFAULT;

CREATE INDEX IF NOT EXISTS connection_details_node_time_idx
  ON connection_details (node_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS connection_details_node_owner_time_idx
  ON connection_details (node_id, owner_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS connection_details_node_domain_time_idx
  ON connection_details (node_id, display_name, observed_at DESC);

CREATE TABLE IF NOT EXISTS proxy_request_details (
  event_id text NOT NULL,
  node_id text NOT NULL REFERENCES nodes(id),
  observed_at timestamptz NOT NULL,
  host text NOT NULL,
  source_ip inet NOT NULL,
  method text NOT NULL,
  status integer NOT NULL CHECK (status BETWEEN 100 AND 599),
  bytes_sent bigint NOT NULL CHECK (bytes_sent >= 0),
  upstream text NOT NULL,
  upstream_owner_id text NOT NULL DEFAULT '',
  duration_ms bigint NOT NULL CHECK (duration_ms >= 0),
  PRIMARY KEY (event_id, observed_at)
) PARTITION BY RANGE (observed_at);

CREATE TABLE IF NOT EXISTS proxy_request_details_default
  PARTITION OF proxy_request_details DEFAULT;

CREATE INDEX IF NOT EXISTS proxy_request_details_node_time_idx
  ON proxy_request_details (node_id, observed_at DESC);
CREATE INDEX IF NOT EXISTS proxy_request_details_node_host_time_idx
  ON proxy_request_details (node_id, host, observed_at DESC);

CREATE TABLE IF NOT EXISTS owner_minute (
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  owner_id text NOT NULL,
  owner_kind text NOT NULL,
  owner_name text NOT NULL,
  direction text NOT NULL,
  bytes bigint NOT NULL CHECK (bytes >= 0),
  connections bigint NOT NULL CHECK (connections >= 0),
  PRIMARY KEY (node_id, bucket, owner_id, direction)
);

CREATE INDEX IF NOT EXISTS owner_minute_node_time_idx
  ON owner_minute (node_id, bucket DESC);

CREATE TABLE IF NOT EXISTS domain_minute (
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  direction text NOT NULL,
  domain text NOT NULL,
  confidence text NOT NULL,
  bytes bigint NOT NULL CHECK (bytes >= 0),
  connections bigint NOT NULL CHECK (connections >= 0),
  requests bigint NOT NULL CHECK (requests >= 0),
  PRIMARY KEY (node_id, bucket, direction, domain, confidence)
);

CREATE INDEX IF NOT EXISTS domain_minute_node_time_idx
  ON domain_minute (node_id, bucket DESC);

CREATE TABLE IF NOT EXISTS flow_minute (
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  direction text NOT NULL,
  owner_id text NOT NULL,
  owner_name text NOT NULL,
  source text NOT NULL,
  destination text NOT NULL,
  domain text NOT NULL,
  confidence text NOT NULL,
  protocol text NOT NULL,
  remote_port integer NOT NULL,
  country_code text NOT NULL DEFAULT '',
  country_name text NOT NULL DEFAULT '',
  asn bigint NOT NULL DEFAULT 0,
  organization text NOT NULL DEFAULT '',
  network_classification text NOT NULL DEFAULT 'unknown',
  bytes bigint NOT NULL CHECK (bytes >= 0),
  connections bigint NOT NULL CHECK (connections >= 0),
  PRIMARY KEY (node_id, bucket, direction, owner_id, source, destination, domain, confidence, protocol, remote_port)
);

CREATE INDEX IF NOT EXISTS flow_minute_node_time_idx
  ON flow_minute (node_id, bucket DESC);

CREATE TABLE IF NOT EXISTS proxy_status_minute (
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  host text NOT NULL,
  status integer NOT NULL,
  bytes bigint NOT NULL CHECK (bytes >= 0),
  requests bigint NOT NULL CHECK (requests >= 0),
  PRIMARY KEY (node_id, bucket, host, status)
);

CREATE INDEX IF NOT EXISTS proxy_status_minute_node_time_idx
  ON proxy_status_minute (node_id, bucket DESC);
