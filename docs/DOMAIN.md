# Domain Model

## Purpose

`liki-agent` turns one user question into a grounded destiny-analysis opinion.
It does not own commerce, accounts, durable conversations, entitlements, or
deterministic calculation. A user question may contain birth data and locale;
the agent validates the question, uses Engine MCP for chart facts, and returns
one structured expert opinion.

## Domain boundary

`internal/domain` contains stable concepts and invariants only. It does not
import HTTP, MCP, ADK, LLM SDK, database, or adapter packages. Application and
adapter layers depend inward on the domain; the domain never depends outward.

The stable domain vocabulary is:

| Concept | Meaning |
|---|---|
| Run | One stateless agent execution identified by `RunID` and `ThreadID` |
| Expert opinion | The normalized result of one analysis |
| Engine tool | An allowlisted deterministic computation exposed over MCP |
| LLM call | One auditable model invocation inside a run |
| Policy version | The safety-policy revision applied to an analysis |
| Prompt version | The prompt revision applied to an analysis |

## ExpertOpinion

`domain.ExpertOpinion` is the stable output contract. It is also represented by
`contracts/expert-opinion.schema.json`.

| Field | Meaning | Invariant |
|---|---|---|
| `expert` | The expert that produced the conclusion | Required |
| `system` | The destiny system, such as Bazi or Ziwei | Required |
| `school` | Optional school or calculation convention | Optional |
| `topic` | Short canonical analysis topic | Optional |
| `conclusion` | Complete user-facing answer | Required, non-empty |
| `confidence` | Degree of confidence in the conclusion | `0.0` through `1.0` |
| `supporting_factors` | Grounded factors supporting the conclusion | Optional |
| `limitations` | Uncertainty, missing data, and safety boundaries | Optional |
| `source_tools` | Engine tools that produced deterministic facts | Must reflect real calls |
| `metadata` | Non-domain runtime provenance | Optional |
| `created_at` | Opinion creation time | Required by the JSON contract |

`ExpertOpinion.Validate()` requires `expert`, `system`, a non-empty
`conclusion`, and confidence in the inclusive range `[0,1]`. Missing user-facing
content is a domain error, not a protocol-specific concern.

## Safety invariants

1. Deterministic chart claims must come from Engine MCP tools.
2. `source_tools` is derived from real tool responses, never from model claims.
3. Conclusions express tendency and uncertainty; they do not issue medical,
   legal, investment, or other professional directives.
4. The agent must not hide missing birth data or material calculation limits.
5. Prompt and policy versions travel with runtime metadata and audit evidence.

## Audit model

`domain.LLMCall` is the durable audit model. One row represents one logical LLM
call and records:

- run, thread, user, product, agent, model, and provider correlation;
- `running`, `completed`, or `failed` status;
- prompt, completion, thought, and total token usage;
- duration, error code, and error message;
- graph, engine-contract, prompt, and policy versions.

`run_id`, `thread_id`, and `user_id` are opaque cross-service identifiers. The
audit ledger intentionally has no foreign keys because users, threads, products,
and payments belong to other services. Failed model calls retain a stable error
code from `internal/domain/error_codes.go`; error codes are never bare literals
outside that source of truth.

## Run state

Run state is execution working state, not a product aggregate. Protocol input
supplies history and identity; ADK session state is scoped to one run and is
not used as durable conversation storage. A crashed or cancelled run is
retried by starting a new run.
