package agent_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
)

type recordingAuditEvents struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAuditEvents) Record(_ context.Context, event *audit.Event) error {
	if event == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *event)
	return nil
}

func (r *recordingAuditEvents) eventsOfType(eventType audit.EventType) []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]audit.Event, 0)
	for _, event := range r.events {
		if event.Type == eventType {
			result = append(result, event)
		}
	}
	return result
}

func newAuditRuntime(t *testing.T) (*agent.Runtime, *recordingAuditEvents) {
	t.Helper()
	events := &recordingAuditEvents{}
	deployment := agent.NewTestDeployment(t)
	t.Setenv("TEST_MCP_ENDPOINT", "http://127.0.0.1:9/mcp")
	runtime, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		ModelAPIKey:      "test-key",
		Deployment:       deployment,
		StructuredOutput: "json_schema",
		GraphVersion:     "test-graph",
		ContractVersion:  "test-contract",
		Provider:         "test-provider",
		AuditRecorder:    events,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime, events
}

func TestExternalAuditRunLifecycleIsExclusive(t *testing.T) {
	runtime, events := newAuditRuntime(t)
	scope := agent.AuditRunScope{RunID: "run_1", ThreadID: "thread_1", UserID: "user_1"}
	ctx := context.Background()
	if err := runtime.BeginAuditRun(ctx, "session_1", scope); err != nil {
		t.Fatalf("BeginAuditRun() error = %v", err)
	}
	if err := runtime.BeginAuditRun(ctx, "session_1", scope); err == nil {
		t.Fatal("second BeginAuditRun unexpectedly succeeded")
	}
	if got := len(events.eventsOfType(audit.EventRunStarted)); got != 1 {
		t.Fatalf("started events = %d, want 1", got)
	}
	if err := runtime.EndAuditRun(ctx, "session_1", nil); err != nil {
		t.Fatalf("EndAuditRun() error = %v", err)
	}
	if got := len(events.eventsOfType(audit.EventRunCompleted)); got != 1 {
		t.Fatalf("completed events = %d, want 1", got)
	}
	scope.RunID = "run_2"
	if err := runtime.BeginAuditRun(ctx, "session_1", scope); err != nil {
		t.Fatalf("BeginAuditRun after EndAuditRun() error = %v", err)
	}
	if err := runtime.EndAuditRun(ctx, "session_1", nil); err != nil {
		t.Fatalf("EndAuditRun after session reuse() error = %v", err)
	}
}

func TestExternalAuditRunAllowsMultipleTasksInOneSession(t *testing.T) {
	runtime, events := newAuditRuntime(t)
	ctx := context.Background()
	for _, runID := range []string{"task_1", "task_2"} {
		scope := agent.AuditRunScope{
			RunID:    runID,
			ThreadID: "context_1",
			UserID:   "user_1",
			Protocol: "a2a",
		}
		if err := runtime.BeginAuditRun(ctx, "context_1", scope); err != nil {
			t.Fatalf("BeginAuditRun(%s) error = %v", runID, err)
		}
		if err := runtime.EndAuditRun(ctx, "context_1", nil); err != nil {
			t.Fatalf("EndAuditRun(%s) error = %v", runID, err)
		}
	}
	terminal := events.eventsOfType(audit.EventRunCompleted)
	if len(terminal) != 2 {
		t.Fatalf("terminal events = %d, want 2", len(terminal))
	}
	if terminal[0].ID == terminal[1].ID ||
		terminal[0].RunID == terminal[1].RunID ||
		terminal[0].Protocol != "a2a" || terminal[1].Protocol != "a2a" {
		t.Fatalf("terminal audit identities = %#v / %#v", terminal[0], terminal[1])
	}
	for _, event := range terminal {
		if event.AgentDefinitionDigest == "" ||
			event.DefinitionDigest == "" ||
			event.Model == "" ||
			event.Provider == "" {
			t.Fatalf("terminal audit lacks provenance: %#v", event)
		}
	}
}

func TestExternalAuditRunRejectsDuplicateRunID(t *testing.T) {
	runtime, _ := newAuditRuntime(t)
	ctx := context.Background()
	scope := agent.AuditRunScope{
		RunID:    "task_duplicate",
		ThreadID: "context_1",
		UserID:   "user_1",
		Protocol: "a2a",
	}
	if err := runtime.BeginAuditRun(ctx, "context_1", scope); err != nil {
		t.Fatalf("BeginAuditRun() error = %v", err)
	}
	if err := runtime.EndAuditRun(ctx, "context_1", nil); err != nil {
		t.Fatalf("EndAuditRun() error = %v", err)
	}
	err := runtime.BeginAuditRun(ctx, "context_2", scope)
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeRunIDConflict {
		t.Fatalf("second BeginAuditRun() error = %v, want %s", err, domain.CodeRunIDConflict)
	}
}

func TestExternalAuditRunRecordsDuration(t *testing.T) {
	runtime, events := newAuditRuntime(t)
	ctx := context.Background()
	scope := agent.AuditRunScope{
		RunID:    "task_duration",
		ThreadID: "context_1",
		UserID:   "user_1",
		Protocol: "a2a",
	}
	if err := runtime.BeginAuditRun(ctx, "context_1", scope); err != nil {
		t.Fatalf("BeginAuditRun() error = %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := runtime.EndAuditRun(ctx, "context_1", nil); err != nil {
		t.Fatalf("EndAuditRun() error = %v", err)
	}
	completed := events.eventsOfType(audit.EventRunCompleted)
	if len(completed) != 1 || completed[0].DurationMS <= 0 {
		t.Fatalf("completed duration = %+v, want positive duration", completed)
	}
}
