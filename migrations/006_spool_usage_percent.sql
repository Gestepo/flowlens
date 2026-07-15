ALTER TABLE collector_health
  ADD COLUMN IF NOT EXISTS usage_percent double precision NOT NULL DEFAULT 0
  CHECK (usage_percent >= 0 AND usage_percent <= 100);
