package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/liki/liki-agent/internal/domain"
)

func TestLLMCallRepositoryPersistsLifecycle(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	now := time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC)
	call := &domain.LLMCall{
		ID: "call_1", RunID: "run_1", ThreadID: "thread_1", UserID: "user_1",
		Product: "liki-agent", AgentName: "chief_analyst", Model: "gpt-test",
		Provider: "openai-compatible", Status: domain.LLMCallRunning,
		GraphVersion: "test-graph", ContractVersion: "test-engine",
		PromptVersion: "test-prompt", PolicyVersion: "test-policy", StartedAt: now,
	}
	repo := NewLLMCallRepository(store.GORM())
	if err := call.Validate(); err != nil {
		t.Fatalf("validate start = %v", err)
	}
	if err := repo.Start(ctx, call); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	call.Complete(domain.LLMTokenUsage{PromptTokens: 100, CompletionTokens: 20, ThoughtTokens: 3, TotalTokens: 123}, now.Add(time.Second))
	if err := repo.Finish(ctx, call); err != nil {
		t.Fatalf("Finish() error = %v", err)
	}

	loaded, err := repo.Get(ctx, call.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if loaded.Status != domain.LLMCallCompleted || loaded.TotalTokens != 123 || loaded.DurationMS != 1000 {
		t.Fatalf("loaded = %+v", loaded)
	}
	if loaded.Product != "liki-agent" || loaded.PromptVersion != "test-prompt" || loaded.PolicyVersion != "test-policy" {
		t.Fatalf("audit context = %+v", loaded)
	}
	calls, err := repo.ListByRun(ctx, call.RunID)
	if err != nil {
		t.Fatalf("ListByRun() error = %v", err)
	}
	if len(calls) != 1 || calls[0].ID != call.ID {
		t.Fatalf("ListByRun() = %+v", calls)
	}
}

func TestLLMCallRepositoryFinishMissing(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	call := &domain.LLMCall{
		ID: "missing", RunID: "run", ThreadID: "thread", UserID: "user",
		AgentName: "agent", Model: "model", Provider: "test",
		Status: domain.LLMCallFailed, StartedAt: time.Now().UTC(),
	}
	err = NewLLMCallRepository(store.GORM()).Finish(context.Background(), call)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("Finish() error = %v, want ErrNotFound", err)
	}
}

func TestAuditMigrationCreatesOnlyAuditBusinessTable(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	var tables []string
	if err := store.GORM().Raw(
		"SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name != 'goose_db_version' ORDER BY name",
	).Scan(&tables).Error; err != nil {
		t.Fatalf("query tables: %v", err)
	}
	if len(tables) != 1 || tables[0] != "agent_llm_calls" {
		t.Fatalf("tables = %v, want [agent_llm_calls]", tables)
	}
}
