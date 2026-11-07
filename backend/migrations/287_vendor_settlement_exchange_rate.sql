-- A supplier has one CNY/USDT rate shared by all group purchase prices.
-- NULL preserves existing profiles without inventing an agreed payment rate.
-- Existing statement snapshots and payments are intentionally untouched.
ALTER TABLE vendors ADD COLUMN IF NOT EXISTS default_exchange_rate NUMERIC(28,10)
    CHECK (default_exchange_rate > 0);
