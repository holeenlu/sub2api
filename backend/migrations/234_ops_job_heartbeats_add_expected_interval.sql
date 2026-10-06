-- Add expected_interval_seconds to ops_job_heartbeats so liveness can be judged per job.

ALTER TABLE IF EXISTS ops_job_heartbeats
    ADD COLUMN IF NOT EXISTS expected_interval_seconds BIGINT;

COMMENT ON COLUMN ops_job_heartbeats.expected_interval_seconds IS
    'Expected seconds between runs as declared by the job; NULL = unknown (legacy rows), 0 = job is currently not scheduled';
