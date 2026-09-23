GO ?= go
VERSION ?= $(shell git describe --tags --always --dirty --match "v*" 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w -X github.com/liki/liki-agent/internal/platform/buildinfo.Version=$(VERSION) -X github.com/liki/liki-agent/internal/platform/buildinfo.Commit=$(COMMIT) -X github.com/liki/liki-agent/internal/platform/buildinfo.BuildTime=$(BUILD_TIME)

ifeq ($(LIKI_ENGINE_MCP_URL),)
LIKI_ENGINE_MCP_URL := http://127.0.0.1:18081/mcp
endif

# Contract tests consume a deployed MCP endpoint and therefore stay outside the
# isolated pre-push test loop.
TEST_PACKAGES ?= $(shell $(GO) list ./... | grep -v '^github.com/liki/liki-agent/tests/contract$$')

.PHONY: help fmt fmt-check vet test check test-db db-backup db-verify build run dev-up dev-down dev contract gate smoke
help:
	@echo "make check    - static checks: formatting and vet"
	@echo "make test     - run isolated tests (no deployed Engine dependency)"
	@echo "make test-db  - run SQLite persistence tests"
	@echo "make gate     - pre-push gate: static checks and isolated tests"
	@echo "make db-backup - create an online SQLite backup"
	@echo "make db-verify - verify the latest SQLite backup"
	@echo "make build    - build ./bin/liki-agent"
	@echo "make contract - Engine MCP contract test against local Engine"
	@echo "make dev-up   - start local Engine MCP"
	@echo "make dev-down - stop local Engine MCP"

fmt:
	gofmt -w cmd internal tests

fmt-check:
	@files="$$(gofmt -l cmd internal tests)"; if [ -n "$$files" ]; then echo "unformatted files:"; echo "$$files"; exit 1; fi

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 $(TEST_PACKAGES)

check: fmt-check vet

test-db:
	$(GO) test -race -count=1 ./internal/audit/sqlite/...

db-backup:
	@bash -eu -o pipefail -c '\
		DB_PATH="$${LIKI_DB_PATH:-./data/liki-agent-audit.db}"; \
		BACKUP_DIR="$${LIKI_BACKUP_DIR:-./backups}"; \
		mkdir -p "$$BACKUP_DIR"; \
		OUT="$$BACKUP_DIR/liki-agent-audit-$$(date +%Y%m%d-%H%M%S).db"; \
		sqlite3 "$$DB_PATH" ".backup '"'"'$$OUT'"'"'"; \
		echo "$$OUT"'

db-verify:
	@bash -eu -o pipefail -c '\
		LATEST="$$(ls -1t "$${LIKI_BACKUP_DIR:-./backups}"/liki-agent-*.db 2>/dev/null | head -1)"; \
		if [ -z "$$LATEST" ]; then echo "no backup found" >&2; exit 1; fi; \
		sqlite3 "$$LATEST" "PRAGMA integrity_check;"'

build:
	$(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/liki-agent ./cmd/agent

run:
	./scripts/dev.sh

contract:
	LIKI_ENGINE_MCP_URL="$(LIKI_ENGINE_MCP_URL)" $(GO) test -race -count=1 ./tests/contract/...

gate: check test

smoke: build contract

dev-up:
	docker compose -f deploy/docker-compose.dev.yml up --build -d engine-mcp

dev-down:
	docker compose -f deploy/docker-compose.dev.yml down --remove-orphans

dev: dev-up
	docker compose -f deploy/docker-compose.dev.yml up --build -d
