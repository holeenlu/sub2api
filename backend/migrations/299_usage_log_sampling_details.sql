-- Durable disjoint sampling evidence for the existing single Cloud turn debit.
-- NULL on historical/native rows: never fabricate missing breakdowns.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS sampling_details JSONB;
