# Runtime Domain Model

`liki-agents` is a generic multi-agent runtime. Prompts, skill workflow, tool
semantics, domain schemas, and domain evaluation belong to the Liki
skill/project.

## Core runtime concepts

| Concept | Meaning |
|---|---|
| AgentDeployment | External deployment artifact containing one or more Agent definitions |
| AgentDefinition | One Agent's instruction, optional output schema, and tool allowlist |
| Run | One stateless execution identified by `RunID` and `ThreadID` |
| Tool execution | One allowlisted MCP tool invocation |
| LLM call | One auditable model invocation inside a run |
| Audit event | Immutable evidence of a lifecycle fact |

## AgentDeployment invariants

1. The artifact is loaded from the filesystem and cannot be uploaded at runtime.
2. Prompt and output schema are external files, not Go defaults.
3. `output.textPointer` selects the user-facing string from validated output.
4. An empty tool allowlist means the Agent has no tool access.
5. Instruction and schema digests identify exact behavior; a plain-text Agent
   has an empty schema digest.
6. Deployment, instruction, and schema digests enter audit evidence.

## Execution invariants

1. Only allowlisted MCP tools can be exposed or invoked.
2. Tool input/output are audited as canonical digests and sizes, never raw payload.
3. Plain text is the default output contract.
4. Structured output is optional and validated against the Agent's external
   JSON Schema.
5. `RunResult.Output` is generic validated JSON for structured Agents.
6. `RunResult.Text` is final model text for plain Agents or the configured
   JSON Pointer value for structured Agents.
7. Failed runs mark pending tool executions as interrupted so every tool has a
   terminal audit event.

## Audit invariants

1. Audit events are append-only at the database boundary.
2. Every run has started and terminal lifecycle evidence.
3. Every model call has started and terminal lifecycle evidence.
4. Every tool call has started and terminal lifecycle evidence, including
   interrupted calls.
5. Audit records version and digest provenance, but not secrets, prompts, raw
   model output, or tool payloads.
6. Audit write failures fail closed; cancellation does not prevent terminal
   audit events.

## Ownership boundary

`liki-web` owns users, products, entitlements, quota, and durable conversations.
The Liki skill/project owns domain behavior and MCP tool semantics.
`liki-agents` owns generic execution, protocol adaptation, evidence, and
operations.
