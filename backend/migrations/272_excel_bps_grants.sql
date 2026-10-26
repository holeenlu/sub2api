-- BPS is explicitly reintroduced by request. Retired platform data and prior
-- cleanup migrations remain unchanged; native Codex credentials are untouched.
CREATE TABLE IF NOT EXISTS openai_excel_grants (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    ciphertext TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
