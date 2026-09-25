GO ?= go
NPM ?= npm
VERSION ?= $(shell git describe --tags --always --dirty --match "v*" 2>/dev/null || echo dev)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_TIME ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS ?= -s -w -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Version=$(VERSION) -X github.com/ml8s/liki-agents/internal/platform/buildinfo.Commit=$(COMMIT) -X github.com/ml8s/liki-agents/internal/platform/buildinfo.BuildTime=$(BUILD_TIME)

TEST_PACKAGES ?= ./...

.PHONY: help fmt fmt-check vet lint-readme test check test-db db-backup db-verify build run dev dev-down validate gate smoke
help:
	@echo "make check    - static checks: formatting and vet"
	@echo "make lint-readme - lint and test developer documentation"
	@echo "make test     - run isolated tests (no deployed Engine dependency)"
	@echo "make test-db  - run SQLite persistence tests"
	@echo "make gate     - pre-push gate: static checks and isolated tests"
	@echo "make db-backup - create an online SQLite backup"
	@echo "make db-verify - verify the latest SQLite backup"
	@echo "make build    - build ./bin/liki-agents"
	@echo "make dev      - run the development Agent and MCP containers in the foreground"
	@echo "make dev-down - remove the development Agent workload"
	@echo "make validate - validate the AgentDeployment artifact without starting the server"

fmt:
	gofmt -w cmd internal contracts tests

fmt-check:
	@files="$$(gofmt -l cmd internal contracts tests)"; if [ -n "$$files" ]; then echo "unformatted files:"; echo "$$files"; exit 1; fi

lint-readme:
	$(NPM) run lint:docs
	$(NPM) run test:docs

vet:
	$(GO) vet ./...

test:
	$(GO) test -race -count=1 $(TEST_PACKAGES)

check: lint-readme fmt-check vet validate

test-db:
	$(GO) test -race -count=1 ./internal/audit/sqlite/...

db-backup:
	@bash -eu -o pipefail -c '\
		DB_PATH="$${LIKI_DB_PATH:-./data/liki-agents-audit.db}"; \
		BACKUP_DIR="$${LIKI_BACKUP_DIR:-./backups}"; \
		mkdir -p "$$BACKUP_DIR"; \
		OUT="$$BACKUP_DIR/liki-agents-audit-$$(date +%Y%m%d-%H%M%S).db"; \
		sqlite3 "$$DB_PATH" ".backup '"'"'$$OUT'"'"'"; \
		echo "$$OUT"'

db-verify:
	@bash -eu -o pipefail -c '\
		LATEST="$$(ls -1t "$${LIKI_BACKUP_DIR:-./backups}"/liki-agents-*.db 2>/dev/null | head -1)"; \
		if [ -z "$$LATEST" ]; then echo "no backup found" >&2; exit 1; fi; \
		sqlite3 "$$LATEST" "PRAGMA integrity_check;"'

build:
	$(GO) build -trimpath -buildvcs=false -ldflags="$(LDFLAGS)" -o bin/liki-agents ./cmd/liki-agents

run:
	./scripts/dev.sh

gate: check test

smoke: build

validate:
	@$(GO) run ./cmd/liki-agents validate -deployment $(if $(LIKI_AGENTS_DEPLOYMENT_FILE),$(LIKI_AGENTS_DEPLOYMENT_FILE),./dev/agent-deployment/deployment.json)

dev-down:
	docker compose --project-directory . -f dev/docker-compose.yml down --remove-orphans

dev:
	docker compose --project-directory . --profile mcp -f dev/docker-compose.yml up --build --abort-on-container-exit
