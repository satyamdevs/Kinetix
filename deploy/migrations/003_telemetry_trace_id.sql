-- Phase: Observability — correlate usage telemetry with distributed traces.
-- Applied manually to databases created with the pre-trace schema:
--   clickhouse-client --query "SOURCE deploy/migrations/003_telemetry_trace_id.sql"
-- Fresh databases get the column from 002 directly.

ALTER TABLE orca_telemetry.chat_completions
    ADD COLUMN IF NOT EXISTS trace_id String DEFAULT '' AFTER error;