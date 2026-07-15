ALTER TABLE flow_minute
  ADD COLUMN IF NOT EXISTS requests bigint NOT NULL DEFAULT 0 CHECK (requests >= 0);
