# liki-agent

`liki-agent` is a standalone destiny-analysis agent runtime. It exposes the
standard A2A and AG-UI protocols and executes one shared ADK graph.

```text
server client ──A2A──────┐
                         ↓
liki-web ──AG-UI SSE→ liki-agent ──MCP→ liki-engine
```

`liki-web` owns users, authentication, products, payments, entitlements,
quota, and abuse control. `liki-agent` owns agent execution, model
orchestration, safety policy, and the LLM call audit ledger. It does not own
commerce, user accounts, conversation-as-a-product storage, or deterministic
destiny computation.

## Public surface

```text
GET  /.well-known/agent-card.json
POST /a2a
POST /ag-ui

GET  /healthz
GET  /readyz
GET  /version
GET  /metrics
```

There is no private `/v1` application API.

## Runtime

- Google ADK Go v2 single-expert graph.
- OpenAI-compatible model provider.
- Engine MCP Streamable HTTP toolset with an allowlist.
- Structured output: answer, confidence, topic, key factors, and limitations.
- Prompt and policy versions attached to expert opinions and LLM audit rows.
- Run-scoped in-memory ADK session state.
- SQLite is audit-only: `agent_llm_calls`.

## Standard protocols

| Protocol | Consumer | Purpose |
|---|---|---|
| A2A | server or Agent client | discovery, tasks, artifacts, cancel, streaming |
| AG-UI | gateway/frontend | browser-facing run and UI event stream |
| MCP | liki-agent | deterministic Engine tools |

See [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) and
[`docs/DOMAIN.md`](docs/DOMAIN.md).

## Security boundary

`liki-agent` is an internal service. Production requires:

```text
Authorization: Bearer <LIKI_AGENT_INTERNAL_TOKEN>
```

For AG-UI, `liki-web` must authenticate the browser and inject:

```text
X-Liki-User-ID: <verified user id>
```

`liki-agent` rejects AG-UI requests without that verified identity. It does not
issue user sessions or trust an unauthenticated browser request.

## Configuration

Copy `.env.example` to `.env`. The required production settings are the public
URL, internal service token, LLM API key, SQLite path, Engine MCP URL, and
Engine tool allowlist. `.env.example` is the complete current configuration
surface.

For Zhipu / BigModel, use:

```text
LIKI_LLM_BASE_URL=https://open.bigmodel.cn/api/v1
LIKI_LLM_PROVIDER=zhipu
```

## Local run

Start the Engine MCP:

```bash
make dev-up
```

Configure and run the agent:

```bash
cp .env.example .env
make run
```

Check readiness:

```bash
curl -sS http://127.0.0.1:8083/readyz | jq
```

## Checks

```bash
make check
```

Run isolated tests:

```bash
make test
```

Run the pre-push gate:

```bash
make gate
```

`make gate` runs static checks and isolated tests for pre-push. It does not
consume real LLM usage or require a deployed Engine.

Validate the deployed Engine contract separately:

```bash
make contract
```

SQLite-specific checks:

```bash
make test-db
```
