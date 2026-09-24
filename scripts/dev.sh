#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

if [[ -z "${LIKI_ENGINE_MCP_URL:-}" ]]; then
  echo "LIKI_ENGINE_MCP_URL is required; point it at the Liki/Engine test MCP endpoint" >&2
  exit 2
fi
export LIKI_ENGINE_MCP_URL
export LIKI_ENV="${LIKI_ENV:-development}"
export LIKI_LOG_LEVEL="${LIKI_LOG_LEVEL:-debug}"

exec go run ./cmd/liki-agents
