-- Telemetry now ends at local storage. Preserve samples and deduplication state;
-- retire metadata belonging exclusively to the removed supplier relay.
ALTER TABLE IF EXISTS ops_client_telemetry_logs
    DROP COLUMN IF EXISTS account_id,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS upstream_status,
    DROP COLUMN IF EXISTS error_code;
