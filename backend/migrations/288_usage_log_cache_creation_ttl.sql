-- Persist observed OpenAI cache-write TTL breakdowns without inferring historical TTLs.
-- These columns are portions of cache_creation_tokens, not additional usage.
ALTER TABLE usage_logs
    ADD COLUMN IF NOT EXISTS cache_creation_30m_tokens integer NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cache_creation_12h_tokens integer NOT NULL DEFAULT 0;
