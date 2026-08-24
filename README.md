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
make run-gateway                     # gateway on :8080

curl localhost:8080/v1/models \
  -H "Authorization: Bearer sk-orca-dev-key"
```

### Endpoints

| Route | Description |
|---|---|
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness |
| `POST /v1/chat/completions` | OpenAI-compatible chat (mock responses in Phase 1) |
| `GET /v1/models` | Model registry |

Auth: `Authorization: Bearer <key>` — configure keys via `ORCA_API_KEYS`.

## Roadmap

- [x] **Phase 1** — Scaffold: gateway server, health checks, mock routes, infra compose
- [ ] **Phase 2** — Provider adapters (OpenAI/Anthropic/Ollama), SSE streaming, failover routing
- [ ] **Phase 3** — Governance: API keys, Redis-backed rate limiting, quotas, guardrails
- [ ] **Phase 4** — Telemetry: ClickHouse ingest, token/cost accounting
- [ ] **Phase 5** — Dashboard: usage charts, latency views, policy editor
- [ ] **Phase 6** — Hardening: <10ms overhead benchmarks, integration tests, CI
