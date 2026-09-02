-- Move the persisted TTFT p99 threshold off the retired 500ms default.
--
-- GetMetricThresholds writes its defaults back to settings on the first read,
-- so every instance that ever opened the ops dashboard has ttft_p99_ms_max=500
-- stored. That value is far below the prefill latency of a normal streaming
-- request, and it now also sets the TTFT scale of the dashboard health score.
-- A deliberately configured 500 is indistinguishable from the stale default,
-- so both are moved to the new 10s default; admins can set it back.

UPDATE settings
SET value = jsonb_set(value::jsonb, '{ttft_p99_ms_max}', to_jsonb(10000))::text,
    updated_at = NOW()
WHERE key = 'ops_metric_thresholds'
  AND value LIKE '{%"ttft_p99_ms_max"%'
  AND (value::jsonb ->> 'ttft_p99_ms_max') IN ('500', '500.0');
