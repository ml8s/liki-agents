#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."
export LIKI_AGENTS_URL="${LIKI_AGENTS_URL:-http://127.0.0.1:8083}"
export LIKI_CHAT_UI_ADDR="${LIKI_CHAT_UI_ADDR:-127.0.0.1:8084}"
exec node dev/chat/server.mjs
