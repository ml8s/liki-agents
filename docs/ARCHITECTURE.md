# Architecture

## System boundary

`liki-agent` is a standalone agent runtime. `liki-web` owns users,
authentication, products, payments, entitlements, quota, and durable product
conversations. `liki-engine` owns deterministic destiny computation.

```text
Server / machine client
  ↓ A2A JSON-RPC
┌────────────┐   MCP Streamable HTTP
│ liki-agent │──────────────────────→ liki-engine
└────────────┘
  ↑ AG-UI SSE
liki-web gateway
```

The service has one execution path and no private `/v1` application API.

## Principles

1. A2A and AG-UI are standard protocol adapters over one shared runtime.
2. Google ADK Go v2 owns the model/tool execution graph.
3. Engine MCP is the only boundary to deterministic calculation.
4. Every Engine tool call is constrained by an explicit allowlist.
5. SQLite stores only the LLM audit ledger.
6. `internal/domain` remains free of transport, provider, adapter, and storage
   imports.

## Dependencies and modules

```text
cmd/agent
  → internal/transport
  → internal/protocol/a2a
  → internal/protocol/agui
  → internal/agent
  → Engine MCP / model provider

internal/audit/sqlite
  → internal/domain

internal/observability/prometheus
  → observability interfaces
```

Protocol adapters never import each other. The agent never imports a protocol
package. Database technology is confined to `internal/audit/sqlite`.

| Module | Responsibility |
|---|---|
| `internal/protocol/a2a` | Agent Card and official A2A JSON-RPC binding |
| `internal/protocol/agui` | Official AG-UI request decoding and SSE binding |
| `internal/agent` | Single ADK graph, model adapter, MCP tools, structured output, audit hooks |
| `internal/domain` | Stable opinions, audit records, errors, and invariants |
| `internal/audit/sqlite` | Migration and persistence for `agent_llm_calls` |
| `internal/observability` | Metric interfaces; Prometheus is the only exporter |
| `internal/platform` | Configuration, identity, health contracts, and build metadata |
| `internal/transport` | Routes, bearer auth, identity propagation, body limit, readiness |

## Runtime execution

Each protocol request is mapped to one agent `RunRequest` and one run-scoped
ADK session. Protocol callers may pass prior history, but the agent does not
store product conversation history.

The runtime uses the official ADK OpenAI-compatible model integration. Zhipu /
BigModel uses provider-safe JSON-object mode while retaining the same
structured output contract. OpenAI-compatible providers can use native JSON
schema mode. Provider differences do not enter the domain or protocol layers.

The Engine toolset is consumed through MCP Streamable HTTP and filtered by
`LIKI_ENGINE_ALLOWED_TOOLS`. Tool calls and responses become protocol facts and
`source_tools`; model assertions do not create tool provenance.

The model must produce a structured analysis. ADK validates the output schema
and stores the normalized result in run-scoped session state. The runtime maps
that result to `domain.ExpertOpinion`. Raw model JSON is not emitted to protocol
observers. Partial events are suppressed, so a tool call is emitted once with
its final arguments and result.

## Protocols

### A2A

```text
GET  /.well-known/agent-card.json
POST /a2a
```

A2A uses the official A2A Go SDK JSON-RPC binding and ADK A2A executor. The
Agent Card declares JSONRPC transport, streaming support, and bearer security.
A2A is intended for server or machine callers.

### AG-UI

```text
POST /ag-ui
Content-Type: application/json
Accept: text/event-stream
```

AG-UI decodes the official `RunAgentInput` and emits official SSE events. The
supported profile is text chat. Unsupported capabilities are rejected, not
silently ignored. Engine tools are server-owned; client tools and multimodal
inputs are outside the profile.

| Runtime fact | AG-UI event |
|---|---|
| accepted run | `RUN_STARTED` |
| Engine call | `TOOL_CALL_START` / `TOOL_CALL_ARGS` |
| Engine response | `TOOL_CALL_RESULT` / `TOOL_CALL_END` |
| final answer | `TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` / `TEXT_MESSAGE_END` |
| success | `RUN_FINISHED` |
| failure | `RUN_ERROR` |

The final structured answer is attached to `RUN_FINISHED`. A run never emits
`RUN_ERROR` after `RUN_FINISHED`.

## Operations

### Public endpoints

```text
GET  /healthz
GET  /readyz
GET  /version
GET  /metrics
```

All POST endpoints enforce a 2 MB body limit.

Readiness checks SQLite and Engine MCP through the official MCP initialization
handshake. It requires the configured MCP revision, server identity, and tool
capability. It does not use a legacy Engine HTTP health endpoint or the retired
MCP `ping` RPC.

### Observability and audit

Prometheus exposes protocol request count and duration, active streams, LLM
calls and tokens, and dependency readiness. Structured logging uses `log/slog`
with stable event names. Logs do not contain user prompts or raw model output.

Completed and failed LLM calls are recorded in SQLite WAL mode. The audit
ledger records usage and version evidence, but not product conversation state.
In-flight runs are not recoverable after a process crash; the client starts a
new run.

### Error model

Stable error codes are constants in `internal/domain/error_codes.go`. Runtime
and audit failures use domain codes; HTTP transport validation returns a small
JSON error envelope with a stable code. A2A failures remain native A2A/JSON-RPC
errors and AG-UI failures remain AG-UI events.
