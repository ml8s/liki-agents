package agent_test

import (
	"context"
	"sync"
	"testing"

	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/audit"
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
	runtime, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		ModelAPIKey:      "test-key",
		Deployment:       agent.NewTestDeployment(t),
		StructuredOutput: "json_schema",
		EngineMCPURL:     "http://127.0.0.1:1/mcp",
		GraphVersion:     "test-graph",
		ContractVersion:  "test-contract",
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
	if err := runtime.BeginAuditRun(ctx, "session_1", scope); err != nil {
		t.Fatalf("BeginAuditRun after EndAuditRun() error = %v", err)
	}
}
