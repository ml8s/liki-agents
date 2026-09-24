package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
)

func TestAuditEventRepositoryAppendsImmutableEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	recorder := NewAuditEventRepository(store.GORM())
	ctx := context.Background()
	occurred := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	started := audit.Event{
		ID: "call_1:started", SchemaVersion: audit.SchemaV1,
		Type: audit.EventLLMCallStarted, OccurredAt: occurred,
		RootRunID: "run_1", RunID: "run_1", ThreadID: "thread_1",
		UserID: "user_1", AgentName: "coordinator",
		Model: "test-model", Provider: "test", Status: audit.StatusRunning,
	}
	if err := started.Validate(); err != nil {
		t.Fatalf("validate started event: %v", err)
	}
	if err := recorder.Record(ctx, &started); err != nil {
		t.Fatalf("Record(started) error = %v", err)
	}

	completed := started
	completed.ID = "call_1:completed"
	completed.Type = audit.EventLLMCallCompleted
	completed.OccurredAt = occurred.Add(time.Second)
	completed.Status = audit.StatusSucceeded
	completed.DurationMS = 1000
	if err := recorder.Record(ctx, &completed); err != nil {
		t.Fatalf("Record(completed) error = %v", err)
	}

	var count int64
	if err := store.GORM().Table("agent_audit_events").Where("run_id = ?", "run_1").Count(&count).Error; err != nil {
		t.Fatalf("count audit events: %v", err)
	}
	if count != 2 {
		t.Fatalf("audit event count = %d, want 2", count)
	}

	err = store.GORM().Exec(`UPDATE agent_audit_events SET status = 'tampered' WHERE id = ?`, started.ID).Error
	if err == nil {
		t.Fatal("updating an audit event unexpectedly succeeded")
	}
	err = store.GORM().Exec(`DELETE FROM agent_audit_events WHERE id = ?`, started.ID).Error
	if err == nil {
		t.Fatal("deleting an audit event unexpectedly succeeded")
	}
}

func TestAuditEventRepositoryRejectsInvalidEvents(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	recorder := NewAuditEventRepository(store.GORM())
	err = recorder.Record(context.Background(), nil)
	if err == nil {
		t.Fatal("Record(nil) unexpectedly succeeded")
	}

	event := audit.Event{ID: "invalid"}
	if err := event.Validate(); err == nil {
		t.Fatal("Validate() unexpectedly succeeded")
	}
	if err := recorder.Record(context.Background(), &event); err == nil {
		t.Fatal("Record(invalid) unexpectedly succeeded")
	}

	var domainErr *domain.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("Record(invalid) error = %v, want domain error", err)
	}
}
