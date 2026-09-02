# Migrations

SQL migrations for PostgreSQL (API keys, quotas) and ClickHouse (telemetry schema).

| File | Target | Content |
|---|---|---|
| `001_api_keys.sql` | PostgreSQL | `api_keys` table — governance records (scopes, limits, expiry) |

## Applying

The migration is idempotent and can be applied against the dev database with:

```bash
psql "$POSTGRES_DSN" -f deploy/migrations/001_api_keys.sql
```

ClickHouse telemetry schema arrives with Phase 4.