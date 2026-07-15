CREATE TABLE IF NOT EXISTS nodes (
  id text PRIMARY KEY,
  name text NOT NULL,
  last_seen_at timestamptz
);

CREATE TABLE IF NOT EXISTS ingest_batches (
  node_id text NOT NULL REFERENCES nodes(id),
  batch_id text NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (node_id, batch_id)
);

CREATE TABLE IF NOT EXISTS interface_deltas (
  event_id text NOT NULL,
  node_id text NOT NULL REFERENCES nodes(id),
  observed_at timestamptz NOT NULL,
  interface text NOT NULL,
  direction text NOT NULL CHECK (direction IN ('inbound', 'outbound', 'internal', 'container_to_container')),
  bytes bigint NOT NULL CHECK (bytes >= 0),
  packets bigint NOT NULL DEFAULT 0 CHECK (packets >= 0),
  PRIMARY KEY (event_id, observed_at)
) PARTITION BY RANGE (observed_at);

CREATE TABLE IF NOT EXISTS interface_deltas_default
  PARTITION OF interface_deltas DEFAULT;

CREATE INDEX IF NOT EXISTS interface_deltas_node_time_idx
  ON interface_deltas (node_id, observed_at DESC);

CREATE TABLE IF NOT EXISTS traffic_minute (
  node_id text NOT NULL REFERENCES nodes(id),
  bucket timestamptz NOT NULL,
  direction text NOT NULL CHECK (direction IN ('inbound', 'outbound', 'internal', 'container_to_container')),
  bytes bigint NOT NULL CHECK (bytes >= 0),
  packets bigint NOT NULL CHECK (packets >= 0),
  PRIMARY KEY (node_id, bucket, direction)
);

CREATE TABLE IF NOT EXISTS collector_health (
  event_id text PRIMARY KEY,
  node_id text NOT NULL REFERENCES nodes(id),
  observed_at timestamptz NOT NULL,
  collector text NOT NULL,
  status text NOT NULL,
  code text NOT NULL,
  dropped_events bigint NOT NULL DEFAULT 0 CHECK (dropped_events >= 0),
  message text NOT NULL DEFAULT ''
);
