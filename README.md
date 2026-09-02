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
   `ORCA_API_KEYS`; with `POSTGRES_DSN` set the `api_keys` table is used. Use
   the `keys.Generate` helper to mint a key, then store only its
   `Hash`, `Prefix`, and limits in `api_keys`.
2. **Rate limiting** — fixed-window request counters per key (per-minute cap),
   stored in Redis (`REDIS_ADDR`). Falls back to an in-memory limiter in dev.
   Responses carry `X-RateLimit-Limit` / `X-RateLimit-Remaining`; excess
   requests get `429 rate_limit_exceeded`.
3. **Quotas** — cumulative token spend is tracked per key. Requests from keys
   that exhausted their monthly token budget get `429 quota_exceeded`; actual
   usage is accounted after each completion (streamed and non-streamed).
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

## Roadmap

- [x] **Phase 1** — Scaffold: gateway server, health checks, mock routes, infra compose
- [x] **Phase 2** — Provider adapters (OpenAI/Anthropic/Ollama), SSE streaming, failover routing
- [x] **Phase 3** — Governance: API keys, Redis-backed rate limiting, quotas, guardrails
- [ ] **Phase 4** — Telemetry: ClickHouse ingest, token/cost accounting
- [ ] **Phase 5** — Dashboard: usage charts, latency views, policy editor
- [ ] **Phase 6** — Hardening: <10ms overhead benchmarks, integration tests, CI
