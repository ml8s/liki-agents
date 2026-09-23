# Security Policy

`liki-agent` is an internal service and must not be exposed directly to the public internet.

Required controls:

1. Internal bearer token between `liki-web` and `liki-agent`.
2. Verified user context supplied by `liki-web`.
3. Engine MCP tool allowlist.
4. No secrets or private prompts in logs.
5. Least-privilege deployment.
