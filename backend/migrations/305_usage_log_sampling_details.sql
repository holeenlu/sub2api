-- Durable disjoint sampling evidence for a Cloud settlement (legacy root or stage).
-- NULL on historical/native rows: never fabricate missing breakdowns.
ALTER TABLE usage_logs ADD COLUMN IF NOT EXISTS sampling_details JSONB;
