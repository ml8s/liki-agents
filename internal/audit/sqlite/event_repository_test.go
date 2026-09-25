package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
)

func TestAuditEventRepositoryAppendsImmutableEvents(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "agent-audit.db")
	store, err := Open(databasePath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	recorder := NewAuditEventRepository(store.GORM())
	ctx := context.Background()
	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatalf("stat sqlite database: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sqlite permissions = %o, want 600", info.Mode().Perm())
	}
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
	exists, err := recorder.RunExists(ctx, started.RunID)
	if err != nil {
		t.Fatalf("RunExists(empty) error = %v", err)
	}
	if exists {
		t.Fatal("RunExists(empty) unexpectedly returned true")
	}
	if err := recorder.Record(ctx, &started); err != nil {
		t.Fatalf("Record(started) error = %v", err)
	}
	exists, err = recorder.RunExists(ctx, started.RunID)
	if err != nil {
		t.Fatalf("RunExists(recorded) error = %v", err)
	}
	if !exists {
		t.Fatal("RunExists(recorded) returned false")
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
	if err := recorder.Record(ctx, &completed); err != nil {
		t.Fatalf("Record(idempotent completed) error = %v", err)
	}
	conflict := completed
	conflict.Type = audit.EventLLMCallFailed
	conflict.Status = audit.StatusFailed
	conflict.ErrorCode = domain.CodeLLMCallFailed
	err = recorder.Record(ctx, &conflict)
	var conflictErr *domain.Error
	if !errors.As(err, &conflictErr) || conflictErr.Code != audit.CodeAuditEventConflict {
		t.Fatalf("Record(conflicting id) error = %v, want %s", err, audit.CodeAuditEventConflict)
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

func TestAuditEventRepositoryPreservesProvenance(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "agent-audit.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	recorder := NewAuditEventRepository(store.GORM())
	event := audit.Event{
		ID:                    "run_1:tool.completed",
		SchemaVersion:         audit.SchemaV1,
		Type:                  audit.EventToolCallCompleted,
		OccurredAt:            time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC),
		RootRunID:             "run_1",
		RunID:                 "run_1",
		ThreadID:              "thread_1",
		UserID:                "user_1",
		Protocol:              "ag_ui",
		TraceID:               "0123456789abcdef0123456789abcdef",
		SpanID:                "0123456789abcdef",
		AgentName:             "worker",
		AgentVersion:          "1.0.0",
		AgentDefinitionDigest: "sha256:agent",
		DefinitionName:        "deployment",
		DefinitionVersion:     "1.0.0",
		DefinitionDigest:      "sha256:deployment",
		ToolCallID:            "call_1",
		ToolName:              "test_tool",
		Model:                 "test-model",
		Provider:              "test",
		Status:                audit.StatusSucceeded,
		Payload:               map[string]any{"output_digest": "sha256:output"},
	}
	if err := recorder.Record(context.Background(), &event); err != nil {
		t.Fatalf("Record() error = %v", err)
	}

	var stored struct {
		Protocol              string
		TraceID               string
		SpanID                string
		AgentDefinitionDigest string
		PayloadJSON           string
	}
	if err := store.GORM().Table("agent_audit_events").
		Select("protocol", "trace_id", "span_id", "agent_definition_digest", "payload_json").
		Where("id = ?", event.ID).
		Scan(&stored).Error; err != nil {
		t.Fatalf("read audit event: %v", err)
	}
	if stored.Protocol != event.Protocol ||
		stored.TraceID != event.TraceID ||
		stored.SpanID != event.SpanID ||
		stored.AgentDefinitionDigest != event.AgentDefinitionDigest {
		t.Fatalf("stored provenance = %#v", stored)
	}
	if !strings.Contains(stored.PayloadJSON, "sha256:output") {
		t.Fatalf("stored payload = %q", stored.PayloadJSON)
	}
}
