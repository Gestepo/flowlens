CREATE TABLE administrators (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  username text NOT NULL UNIQUE CHECK (length(username) BETWEEN 1 AND 128),
  password_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE browser_sessions (
  token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
  administrator_id smallint NOT NULL REFERENCES administrators(id) ON DELETE CASCADE,
  csrf_token text NOT NULL CHECK (length(csrf_token) >= 32),
  created_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  idle_expires_at timestamptz NOT NULL,
  expires_at timestamptz NOT NULL,
  CHECK (idle_expires_at > created_at),
  CHECK (expires_at > created_at)
);

CREATE INDEX browser_sessions_expiry_idx ON browser_sessions (expires_at);

CREATE TABLE alert_rules (
  id text PRIMARY KEY,
  kind text NOT NULL,
  name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  severity text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
  config jsonb NOT NULL DEFAULT '{}'::jsonb,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE alerts (
  id bigserial PRIMARY KEY,
  rule_id text NOT NULL REFERENCES alert_rules(id),
  fingerprint text NOT NULL,
  severity text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
  status text NOT NULL CHECK (status IN ('open', 'resolved')),
  node_id text NOT NULL REFERENCES nodes(id),
  owner_id text,
  title text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '{}'::jsonb,
  observed_value double precision NOT NULL,
  comparison_value double precision,
  window_seconds integer NOT NULL CHECK (window_seconds > 0),
  first_seen_at timestamptz NOT NULL,
  last_seen_at timestamptz NOT NULL,
  resolved_at timestamptz,
  occurrence_count bigint NOT NULL DEFAULT 1 CHECK (occurrence_count > 0),
  missing_windows integer NOT NULL DEFAULT 0 CHECK (missing_windows >= 0)
);

CREATE UNIQUE INDEX alerts_one_open_fingerprint_idx ON alerts (fingerprint) WHERE status = 'open';
CREATE INDEX alerts_status_last_seen_idx ON alerts (status, last_seen_at DESC);

CREATE TABLE webhook_settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  enabled boolean NOT NULL DEFAULT false,
  endpoint text NOT NULL DEFAULT '',
  encrypted_secret bytea,
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE webhook_deliveries (
  id bigserial PRIMARY KEY,
  alert_id bigint REFERENCES alerts(id) ON DELETE CASCADE,
  event_type text NOT NULL,
  payload jsonb,
  status text NOT NULL CHECK (status IN ('pending', 'leased', 'delivered', 'terminal', 'cancelled')),
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  lease_owner text,
  lease_expires_at timestamptz,
  response_status integer,
  response_excerpt text NOT NULL DEFAULT '',
  last_error text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  delivered_at timestamptz
);

CREATE INDEX webhook_deliveries_work_idx ON webhook_deliveries (status, next_attempt_at);

CREATE TABLE job_leases (
  name text PRIMARY KEY,
  lease_owner text,
  lease_expires_at timestamptz,
  last_success_at timestamptz,
  last_error text NOT NULL DEFAULT '',
  next_run_at timestamptz NOT NULL
);

CREATE TABLE operation_settings (
  id smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
  detail_retention_days integer NOT NULL DEFAULT 30 CHECK (detail_retention_days BETWEEN 1 AND 30),
  aggregate_retention_months integer NOT NULL DEFAULT 12 CHECK (aggregate_retention_months BETWEEN 1 AND 12),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE traffic_rollups (
  resolution text NOT NULL CHECK (resolution IN ('hour', 'day')),
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  dimension_kind text NOT NULL,
  dimension_key text NOT NULL,
  direction text NOT NULL,
  status_code integer NOT NULL DEFAULT 0,
  bytes bigint NOT NULL CHECK (bytes >= 0),
  packets bigint NOT NULL DEFAULT 0 CHECK (packets >= 0),
  connections bigint NOT NULL DEFAULT 0 CHECK (connections >= 0),
  requests bigint NOT NULL DEFAULT 0 CHECK (requests >= 0),
  source_min_at timestamptz NOT NULL,
  source_max_at timestamptz NOT NULL,
  PRIMARY KEY (resolution, node_id, bucket, dimension_kind, dimension_key, direction, status_code)
);

CREATE INDEX traffic_rollups_query_idx ON traffic_rollups (node_id, resolution, bucket DESC);

INSERT INTO operation_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
INSERT INTO webhook_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING;
INSERT INTO alert_rules (id,kind,name,enabled,severity,config) VALUES
  ('absolute-rate','absolute_rate','传输速率过高',true,'warning','{"threshold":100000000,"window_seconds":300}'),
  ('owner-baseline','owner_baseline','所有者流量偏离基线',true,'warning','{"multiplier":5,"window_seconds":300}'),
  ('new-destination','new_destination','发现新的远程目标',true,'info','{"window_seconds":300}'),
  ('domain-coverage','domain_coverage','域名识别率过低',true,'warning','{"threshold":50,"window_seconds":300}'),
  ('agent-stale','agent_stale','采集节点离线',true,'critical','{"threshold":60,"window_seconds":300}'),
  ('collector-unhealthy','collector_unhealthy','采集器异常',true,'warning','{"window_seconds":300}'),
  ('buffer-pressure','buffer_pressure','Agent 缓冲区压力',true,'warning','{"threshold":80,"window_seconds":300}'),
  ('webhook-failures','webhook_failures','Webhook 连续失败',true,'warning','{"threshold":3,"window_seconds":300}')
ON CONFLICT (id) DO NOTHING;
