# Security Policy

`liki-agents` is an internal runtime and must not be exposed directly to the
public internet.

Required controls:

- Internal bearer token between the gateway and `liki-agents` outside local
  development.
- The same internal token for protocol calls and `/metrics`.
- Verified user context supplied by the gateway for AG-UI.
- Bounded per-source throttling of invalid bearer or identity attempts.
- Mandatory deployment digest pinning outside development.
- HTTPS-only LLM endpoints outside development and no credentials embedded in
  configured URLs.
- Explicit per-Agent MCP tool allowlists.
- No secrets, prompts, raw model output, or tool payloads in logs.
- Least-privilege, non-root deployment.

## Reporting a vulnerability

Do not open a public issue for a security vulnerability. Use GitHub's private
vulnerability reporting for this repository.
