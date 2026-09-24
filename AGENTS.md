# Liki Agents Working Rules

1. Keep `internal/domain` free of HTTP, MCP, LLM SDK, and storage imports.
2. Add capabilities through ports, not by importing adapters from domain or app.
3. Never import `liki` source; consume deployed Engine MCP only.
4. Never call Python Skills from the production Agent path.
5. Every Engine tool call must be allowlisted.
6. Every run must use stable event names and error codes.
7. Never commit secrets.
8. Run `make check` before completing Go changes.
9. Do not commit or push without explicit instruction.
