#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

export LIKI_ENGINE_MCP_URL="${LIKI_ENGINE_MCP_URL:-http://127.0.0.1:18081/mcp}"
export LIKI_ENV="${LIKI_ENV:-development}"
export LIKI_LOG_LEVEL="${LIKI_LOG_LEVEL:-debug}"

exec go run ./cmd/agent
