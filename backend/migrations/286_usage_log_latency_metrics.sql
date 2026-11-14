-- Versioned request/turn latency snapshot; historical rows retain NULL.
-- Nullable with no default avoids rewriting the partitioned usage history.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS latency_metrics JSONB;
