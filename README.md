# Orca

Unified, OpenAI-compatible LLM orchestration platform — model routing, observability, and governance. Orca sits between your applications and downstream AI providers (OpenAI, Anthropic, Ollama/vLLM), acting as a lightweight control plane for inference: request proxying, fallback routing, rate limiting, and cost tracking.

## Architecture

```
                ┌──────────────────────────────┐
 your apps  ───▶│         Orca Gateway         │───▶ OpenAI
 (OpenAI API)   │  proxy · routing · guardrails │───▶ Anthropic
                └──────────────┬───────────────┘───▶ Ollama / vLLM
                               │
        ┌──────────┴───────────┐
        ▼                      ▼
      Redis                 PostgreSQL          ClickHouse
   rate limits            keys & policies     telemetry & costs
```

| Component | Tech | Purpose |
|---|---|---|
| `services/gateway` | Go + chi | Low-latency proxying, router policies, guardrails, SSE streaming |
| `services/dashboard` | Next.js | Metrics, latency/cost visualization, policy configuration |
| `deploy/` | Docker Compose | Postgres (metadata) · Redis (rate limits) · ClickHouse (telemetry) |

## Quickstart

```bash
cp deploy/.env.example deploy/.env   # dev defaults work as-is
make dev-deps                        # start postgres + redis + clickhouse
psql "$POSTGRES_DSN" -f deploy/migrations/001_api_keys.sql   # (optional) governance schema
make run-gateway                     # gateway on :8080

curl localhost:8080/v1/models \
  -H "Authorization: Bearer sk-orca-dev-key"
```

### Endpoints

| Route | Description |
|---|---|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness |
| `POST /v1/chat/completions` | OpenAI-compatible chat (non-streaming and SSE streaming) |
| `GET /v1/models` | Model + fallback-alias registry |

Auth: `Authorization: Bearer <key>` — keys are validated against the governance
store (Postgres `api_keys` table, or in-memory seeded from `ORCA_API_KEYS`).

### Governance (Phase 3)

Every `/v1` request passes through the governance pipeline:

1. **API keys** — Bearer tokens are hashed (SHA-256) and looked up in the key
   store. Disabled and expired keys are rejected. In dev, keys come from
   `ORCA_API_KEYS`; with `POSTGRES_DSN` set the `api_keys` table is used.
   Provision keys with the admin CLI (`make build-keys` → `bin/orca-keys`):

   ```bash
   bin/orca-keys -dsn "$POSTGRES_DSN" create \
     -name my-app -owner team-a -rpm 1000 -tpm 50000000 -ttl 720h
   bin/orca-keys -dsn "$POSTGRES_DSN" list          # view keys
   bin/orca-keys -dsn "$POSTGRES_DSN" disable <id>  # revoke temporarily
   bin/orca-keys -dsn "$POSTGRES_DSN" revoke  <id>  # delete permanently
   ```

   The raw key is printed once at `create`; persist it in a secrets manager.
2. **Rate limiting** — fixed-window request counters per key (per-minute cap),
   stored in Redis (`REDIS_ADDR`). Falls back to an in-memory limiter in dev.
   Responses carry `X-RateLimit-Limit` / `X-RateLimit-Remaining`; excess
   requests get `429 rate_limit_exceeded`.
3. **Quotas** — cumulative token spend is tracked per key. Requests from keys
   that exhausted their monthly token budget get `429 quota_exceeded`; actual
   usage is accounted after each completion (streamed and non-streamed). The
   monthly budget is reset by deleting the per-key spend counter in the limit
   store (handled by a quota-rollover job; not polled automatically yet).
4. **Guardrails** — optional content filters (`ORCA_GUARDRAILS`) evaluated
   against inbound prompts and non-streamed model output. Matches are blocked
   with `400 content_policy_violation`.

Per-key limits (`requests_per_minute`, `tokens_per_month`,
`tokens_per_request`) live on the key record; gateway-wide defaults come from
`ORCA_DEFAULT_RPM` / `ORCA_DEFAULT_TPM`.

### Models

Registered from env; providers absent (e.g. `OPENAI_API_KEY` unset) are skipped:

- OpenAI (`OPENAI_API_KEY`): `gpt-4o`, `gpt-4o-mini`, `gpt-4`, `gpt-4-turbo`, `gpt-3.5-turbo`
- Anthropic (`ANTHROPIC_API_KEY`): `claude-3-5-sonnet-20241022`, `claude-3-5-haiku-20241022`
- Ollama/vLLM (`OLLAMA_BASE_URL`, default `http://localhost:11434/v1`): `llama3.2`, `llama3.1`, `mistral`, `qwen2.5`
- Fallback aliases route in order until one succeeds:
  `orca-fast` = `gpt-4o-mini` → `claude-3-5-haiku` → `llama3.2`
  `orca-smart` = `gpt-4o` → `claude-3-5-sonnet` → `llama3.1`

Streaming: set `"stream": true` in the body to receive OpenAI-compatible
`chat.completion.chunk` events terminated by `data: [DONE]`.

### Telemetry (Phase 4)

Every completion (streamed and non-streamed) records a row to ClickHouse:
timing, key, model, provider, token usage, and USD cost (priced per model).
With `CLICKHOUSE_DSN` unset the gateway uses a bounded in-memory buffer, so telemetry
never blocks or fails a request. Example:

```sql
SELECT model,
       toStartOfDay(ts)          AS day,
       sum(total_tokens)         AS tokens,
       sum(cost_usd)             AS cost,
       avg(latency_ms)           AS avg_ms,
       quantile(0.95)(latency_ms) AS p95_ms
  FROM orca_telemetry.chat_completions
 GROUP BY model, day ORDER BY day DESC;
```

### Dashboard & Admin API (Phase 5)

Run the dashboard (Next.js on :3000 by default). It reads `ORCA_GATEWAY_URL`
and `ORCA_ADMIN_KEY` from the environment; the admin key's record must carry
the `admin` scope (`ORCA_ADMIN_KEY` in the in-memory seed, or via
`orca-keys set-scopes`). Each `/admin/**` route also requires that scope.

```bash
cp services/dashboard/.env.example services/dashboard/.env.local
make dashboard-install
make dashboard-dev          # http://localhost:3000
```

Admin endpoints (all under `/admin`, bearer-auth + admin scope):

| Route | Description |
|---|---|
| `GET /admin/usage/summary` | Per-day, optional per-model usage: requests, tokens, USD cost, avg/p95 latency |
| `GET /admin/keys` | List key records (name, owner, prefix, state, limits) |
| `POST /admin/keys` | Provision a key; returns the raw secret exactly once |
| `GET /admin/keys/{id}` | Fetch a single key record |
| `PUT /admin/keys/{id}/enable` / `disable` | Enable or disable a key |
| `DELETE /admin/keys/{id}` | Revoke a key permanently |

Dashboard pages:
- **Usage** (`/`) — total requests/tokens/cost KPI cards, requests-per-day bar
  chart, and a daily totals table.
- **Latency** (`/latency`) — avg and p95 latency per model.
- **Policies** (`/policies`) — create keys with per-minute/per-month limits and
  TTL, toggle enable/disable, and revoke keys.

With `POSTGRES_DSN` unset, key CRUD runs against the in-memory store seeded
from `ORCA_API_KEYS` / `ORCA_ADMIN_KEY`; with it set, the `api_keys` table is
used so changes persist across restarts.

## Roadmap

- [x] **Phase 1** — Scaffold: gateway server, health checks, mock routes, infra compose
- [x] **Phase 2** — Provider adapters (OpenAI/Anthropic/Ollama), SSE streaming, failover routing
- [x] **Phase 3** — Governance: API keys, Redis-backed rate limiting, quotas, guardrails
- [x] **Phase 4** — Telemetry: ClickHouse ingest, token/cost accounting
- [x] **Phase 5** — Dashboard: usage charts, latency views, policy editor
- [x] **Phase 6** — Hardening: <10ms overhead benchmarks, integration tests, CI

## Testing & Hardening (Phase 6)

Quality gates are exercised in CI (`.github/workflows/ci.yml`) and locally via
`make`.

```bash
make test              # full gateway unit + integration suite
make integration-test  # HTTP-level failover + admin key lifecycle paths
make bench             # overhead benchmark (Phase 6 target)
```

**Overhead benchmark** — `services/gateway/internal/api/benchmark_test.go`
drives the complete auth → rate-limit → quota → route pipeline against an
instantaneous fake provider, isolating the gateway's own proxy overhead from
backing-provider latency. On a development laptop this runs well under the
10ms target (roughly 0.01ms non-streamed, ~0.02ms streamed), reaffirming the
failover routing adds negligible latency.

**Integration tests** — `integration_test.go` exercises the end-to-end HTTP
path: a dead primary upstream transparently falls through to a healthy
fallback, and requests fail with `502` only when every upstream is down.

**CI** — on push to `main` and pull requests: Go `vet` + `build` + tests +
overhead benchmark for the gateway, and `next build` (which runs full
type-checks) for the dashboard.
