# Liki Agents

`liki-agents` is a generic multi-agent runtime for externally defined
`AgentDeployment` artifacts. It executes an ADK Agent tree, orchestrates model
calls and MCP tools, and exposes standard machine and browser protocols.

```text
machine client ──A2A JSON-RPC──┐
                               ↓
liki-web ──AG-UI SSE→ liki-agents ──MCP→ external tools
```

The runtime is domain-neutral. Deployments provide instructions, output
contracts, delegation topology, and tool allowlists. `liki-agents` provides
execution, protocol adaptation, security boundaries, evidence, and
observability.

## Install

Requires Go 1.26+, Docker Compose for container development, and Node.js 18+
for documentation checks. Clone the repository and run `make build`; the binary
is written to `bin/liki-agents`.

## Quick start

Copy `.env.example` to `.env`; set `LIKI_LLM_API_KEY` and every endpoint
variable named by `mcpServers`. Local development may leave the internal token
empty to disable bearer authentication. Then run:

```bash
make run
curl -sS http://127.0.0.1:8083/readyz | jq
```

`make run` starts only the runtime; MCP servers are external dependencies.
`make dev` builds and starts the development Agent plus the Engine and Counsel
MCP fixtures in Compose. For browser-style local testing, run
`scripts/dev-chat-ui.sh` and open <http://127.0.0.1:8084>. That UI is
development-only.

## Runtime model

An `AgentDeployment` is the sole source of Agent behavior and topology. Each
Agent has a description, ADK mode, external instruction file, optional output
contract, and explicit `tools.allow`. `mcpServers` declares logical MCP
dependencies and the environment variables that bind their endpoint and optional
token at runtime. The unique Agent with no incoming `sub_agents` edge is the
entrypoint. ADK owns delegation and execution.

Prompts are external instruction files. Plain text is the default output. For
typed machine-to-machine output, an Agent may declare both a JSON Schema and a
JSON Pointer to the user-facing string. Tool access is denied unless the tool is
listed in that Agent's server-scoped `tools.allow` map.

The artifact is validated against
[`contracts/agent-definition.schema.json`](contracts/agent-definition.schema.json);
external boundaries follow [`docs/STANDARDS.md`](docs/STANDARDS.md). Its digest
covers the manifest, referenced instruction and schema files, and the ADK graph
semantics.

## Protocols

| Consumer | Protocol | Endpoint | Purpose |
|---|---|---|---|
| Machine client | A2A JSON-RPC | `POST /a2a` | discovery, invocation, streaming |
| Browser / gateway | AG-UI SSE | `POST /ag-ui` | chat, tool events, sub-agent lifecycle |
| Machine discovery | A2A Agent Card | `GET /.well-known/agent-card.json` | standard capability metadata |
| Tool server | MCP Streamable HTTP | outbound | deterministic tools |

Operational endpoints are `/readyz`, `/version`, and `/metrics`.
There is no private application API and no ADK Launcher control plane.

## Security boundary

Production protocol calls require the internal service token:

```text
Authorization: Bearer <LIKI_AGENTS_INTERNAL_TOKEN>
```

AG-UI additionally requires `liki-web` to authenticate the browser user and
inject:

```text
X-Liki-User-ID: <verified user id>
```

The runtime does not expose itself directly to the public internet, authenticate
end users, issue sessions, store product conversations, or trust unverified
browser identities.

Local development may leave `LIKI_AGENTS_INTERNAL_TOKEN` empty. Bearer
authentication is then disabled, but AG-UI still requires `X-Liki-User-ID`.

MCP access is allowlisted per Agent and per server. Logs, traces, and audit
records do not contain prompts, raw model output, or tool payloads.

## Configuration

`.env.example` is the complete configuration surface. Important groups are:

| Prefix / variable | Purpose |
|---|---|
| `LIKI_AGENTS_*` | service address, token, data path, artifact and optional digest pin |
| `LIKI_DB_PATH` | SQLite audit database |
| `LIKI_MCP_*`, `LIKI_*_MCP_TOKEN` | endpoint/token bindings for logical MCP servers |
| `LIKI_TOOL_CONTRACT_VERSION`, `LIKI_MAX_CONCURRENT_RUNS` | provenance and bounded concurrent executions |
| `LIKI_LLM_*` | OpenAI-compatible model provider |
| `LIKI_LOG_*` | structured logging |
| `OTEL_*` | standard OpenTelemetry tracing settings |

MCP endpoint values are complete URLs for the selected network. The gateway may
own service prefixes (`/engine/mcp`, `/counsel/mcp/bazi`) while direct
service-network URLs use the MCP service paths (`/mcp`, `/mcp/bazi`). The
runtime does not rewrite MCP paths or add private protocol headers.

Use an empty `LIKI_LLM_STRUCTURED_OUTPUT` for plain-text deployments;
`json_schema` and `json_object` are provider-specific strict/JSON modes.

## Operations

Use `make run` for the host process and `make dev` for the containerized
development service and MCP fixtures. `make dev-down` removes the Compose
workload; `make db-backup` creates an online SQLite backup.
The current runtime supports one process/replica; see
[`docs/STANDARDS.md`](docs/STANDARDS.md) for the prerequisites for scaling it.

## Observability

Prometheus exposes protocol latency, active streams, model calls and tokens,
tool calls and durations, delegation activity, and dependency readiness. Logs
use `log/slog` with stable event names. Distributed tracing uses W3C context
propagation and standard OTLP export. Tracing is disabled unless a standard
OTLP endpoint is configured:

```text
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=http://otel-collector:4318/v1/traces
```

SQLite stores append-only audit events with execution metadata, versions,
digests, duration, and stable error codes. Audit records do not store
conversation or domain payloads.

## Documentation

| Document | Purpose |
|---|---|
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | runtime architecture and protocol mapping |
| [`docs/DOMAIN.md`](docs/DOMAIN.md) | stable runtime domain invariants |
| [`contracts/agent-definition.schema.json`](contracts/agent-definition.schema.json) | deployment contract |
| [`SECURITY.md`](SECURITY.md) | security policy and required controls |
| [`CONTRIBUTING.md`](CONTRIBUTING.md) | development workflow and review gate |
| [`LICENSE`](LICENSE) | open-source license |

## Development

Run static checks, artifact validation, race tests, and build:

```bash
make check
make validate
make test
make gate
make build
```

`make gate` is the pre-push gate. It runs documentation linting, Go static
checks, isolated race tests, and deployment validation. It does not call a real
model provider or external MCP.

## Project boundary

`liki-agents` is not an Agent management control plane. It does not provide
Agent CRUD APIs, deployment registries, tenant management, quota enforcement,
approval workflows, or a management console.

`liki-web` owns users, products, entitlements, quotas, and durable product
conversations. A Liki domain release owns prompts, skills, workflow policy, and
tool semantics; Engine and Counsel own deterministic MCP implementations.
