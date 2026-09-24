# Security Policy

`liki-agents` is an internal runtime and must not be exposed directly to the
public internet.

Required controls:

- Internal bearer token between the gateway and `liki-agents` outside local
  development.
- Verified user context supplied by the gateway for AG-UI.
- Explicit per-Agent MCP tool allowlists.
- No secrets, prompts, raw model output, or tool payloads in logs.
- Least-privilege, non-root deployment.

## Reporting a vulnerability

Do not open a public issue for a security vulnerability. Use GitHub's private
vulnerability reporting for this repository.
