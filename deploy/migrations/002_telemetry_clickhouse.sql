-- Phase 4: telemetry schema (ClickHouse)
-- One row per completion (streamed or not). Applied by the gateway at startup
-- via CLICKHOUSE_DSN.

CREATE TABLE IF NOT EXISTS orca_telemetry.chat_completions
(
    ts             DateTime('UTC'),
    day            Date,
    request_id     String,
    api_key_id     String,
    model          String,
    provider       String,
    streamed       UInt8,
    status         UInt16,       -- HTTP status of the completed request
    prompt_tokens  UInt32,
    completion_tokens UInt32,
    total_tokens   UInt32,
    cost_usd       Float64,
    latency_ms     UInt32,
    error          String,
    trace_id       String        -- OpenTelemetry trace id for correlation
)
ENGINE = MergeTree
PARTITION BY toYYYYMM(day)
ORDER BY (ts, api_key_id, model)
TTL day + INTERVAL 90 DAY;