# Liki Agent evaluations

This directory contains deterministic evaluation cases for the chief analyst.
The current checks validate case shape and stable behavioral boundaries. They
do not assert exact model prose, because that would make useful prompt
improvements unnecessarily brittle.

Each case records:

- the grounded question and birth data passed to the runtime;
- Engine tools that must be used for deterministic chart claims;
- phrases that must never occur in the final answer;
- the structured output contract that the run must satisfy.

Run the cases with:

```bash
go test -race -count=1 ./evals
```
