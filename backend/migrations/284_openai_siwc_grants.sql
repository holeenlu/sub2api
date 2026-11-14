-- SIWC is an independent administrator-selected OpenAI authorization. Native
-- Codex and Excel grants remain unchanged. Registration survives disconnection.
CREATE TABLE IF NOT EXISTS openai_siwc_installation (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    host_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS openai_siwc_grants (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    registration_ciphertext TEXT NOT NULL,
    credentials_ciphertext TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
-- Preserve existing execution classifications and immutability. This is an
-- additive constraint replacement; no historical rows are backfilled.
ALTER TABLE usage_logs DROP CONSTRAINT IF EXISTS usage_logs_authorization_mode_check;
ALTER TABLE usage_logs ADD CONSTRAINT usage_logs_authorization_mode_check
    CHECK (authorization_mode IN ('native', 'bps', 'siwc'));
