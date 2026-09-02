# Migrations

SQL migrations for PostgreSQL (API keys, quotas) and ClickHouse (telemetry schema).

| File | Target | Content |
|---|---|---|
| `001_api_keys.sql` | PostgreSQL | `api_keys` table — governance records (scopes, limits, expiry) |
| `002_telemetry_clickhouse.sql` | ClickHouse | `chat_completions` telemetry table (usage, cost, latency, TTL 90d) |

## Applying

Both migrations are idempotent and can be applied against the dev databases:

```bash
psql "$POSTGRES_DSN" -f deploy/migrations/001_api_keys.sql

# ClickHouse native protocol, e.g.
clickhouse-client --multiquery < deploy/migrations/002_telemetry_clickhouse.sql
```

The gateway connects to ClickHouse via `CLICKHOUSE_DSN`; when unset it keeps
telemetry in a bounded in-memory buffer.