package agent

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
)

type fakeAgentContext struct {
	adkagent.Context
	sessionID      string
	invocationID   string
	agentName      string
	branch         string
	functionCallID string
	traceContext   context.Context
}

func (c *fakeAgentContext) SessionID() string      { return c.sessionID }
func (c *fakeAgentContext) InvocationID() string   { return c.invocationID }
func (c *fakeAgentContext) AgentName() string      { return c.agentName }
func (c *fakeAgentContext) Branch() string         { return c.branch }
func (c *fakeAgentContext) FunctionCallID() string { return c.functionCallID }
func (c *fakeAgentContext) Deadline() (time.Time, bool) {
	if c.traceContext == nil {
		return time.Time{}, false
	}
	return c.traceContext.Deadline()
}

func (c *fakeAgentContext) Done() <-chan struct{} {
	if c.traceContext == nil {
		return nil
	}
	return c.traceContext.Done()
}

func (c *fakeAgentContext) Err() error {
	if c.traceContext == nil {
		return nil
	}
	return c.traceContext.Err()
}

func (c *fakeAgentContext) Value(key any) any {
	if c.traceContext != nil {
		if value := c.traceContext.Value(key); value != nil {
			return value
		}
	}
	return context.Background().Value(key)
}

type stubAuditRecorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *stubAuditRecorder) Record(_ context.Context, event *audit.Event) error {
	if event == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *event)
	return nil
}

func (r *stubAuditRecorder) eventsOfType(eventType audit.EventType) []audit.Event {
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

func (r *stubAuditRecorder) lastEvent() audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.events[len(r.events)-1]
}

func newTestLedger() (*llmLedger, *stubAuditRecorder) {
	recorder := &stubAuditRecorder{}
	ledger := newLLMLedger(recorder, nil, "test", NewTestDeployment(&testing.T{}), func() time.Time {
		return time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	})
	return ledger, recorder
}

func newTestScope() *llmRunScope {
	return &llmRunScope{
		runID:             "run_1",
		threadID:          "thread_1",
		userID:            "user_1",
		agentName:         "coordinator",
		model:             "test-model",
		graph:             "test-graph",
		contract:          "test-contract",
		instructionDigest: "test-instruction",
		definitionName:    "test-definition",
		definitionVersion: "1.0.0",
		definitionDigest:  "sha256:test",
		lastByModel:       make(map[string]llmCallRuntime),
	}
}

func testCtx() *fakeAgentContext {
	return &fakeAgentContext{sessionID: "session_1", invocationID: "inv_1", agentName: "coordinator"}
}

func TestLedgerLifecycleCompletesNormally(t *testing.T) {
	ledger, recorder := newTestLedger()
	scope := newTestScope()
	if !ledger.begin("session_1", scope) {
		t.Fatal("begin() returned false")
	}
	ctx := testCtx()
	if _, err := ledger.beforeModel(ctx, &model.LLMRequest{Model: "test-model"}); err != nil {
		t.Fatalf("beforeModel() error = %v", err)
	}
	if got := len(recorder.eventsOfType(audit.EventLLMCallStarted)); got != 1 {
		t.Fatalf("started events = %d, want 1", got)
	}
	response := &model.LLMResponse{ModelVersion: "test-model"}
	if _, err := ledger.afterModel(ctx, response, nil); err != nil {
		t.Fatalf("afterModel() error = %v", err)
	}
	if got := len(recorder.eventsOfType(audit.EventLLMCallCompleted)); got != 1 {
		t.Fatalf("completed events = %d, want 1", got)
	}
	last := recorder.lastEvent()
	if last.Status != audit.StatusSucceeded {
		t.Fatalf("status = %s, want succeeded", last.Status)
	}
	ledger.end("session_1", nil)
	if got := len(recorder.eventsOfType(audit.EventLLMCallFailed)); got != 0 {
		t.Fatalf("failed events after clean end = %d, want 0", got)
	}
}

func TestLedgerLifecycleFailurePath(t *testing.T) {
	ledger, recorder := newTestLedger()
	scope := newTestScope()
	ledger.begin("session_1", scope)
	ctx := testCtx()
	if _, err := ledger.beforeModel(ctx, &model.LLMRequest{Model: "test-model"}); err != nil {
		t.Fatalf("beforeModel() error = %v", err)
	}
	runErr := domain.NewError(domain.CodeRuntimeTimeout, "LLM timed out", nil)
	_, modelErr := ledger.onModelError(ctx, &model.LLMRequest{Model: "test-model"}, runErr)
	if modelErr == nil {
		t.Fatal("onModelError() should propagate the request error")
	}
	if got := len(recorder.eventsOfType(audit.EventLLMCallFailed)); got != 1 {
		t.Fatalf("failed events = %d, want 1", got)
	}
	last := recorder.lastEvent()
	if last.ErrorCode != domain.CodeRuntimeTimeout {
		t.Fatalf("error_code = %s, want %s", last.ErrorCode, domain.CodeRuntimeTimeout)
	}
	ledger.end("session_1", nil)
	if got := len(recorder.eventsOfType(audit.EventLLMCallFailed)); got != 1 {
		t.Fatalf("failed events after end = %d, want 1", got)
	}
}

func TestLedgerInterruptedRunFailsActiveCalls(t *testing.T) {
	ledger, recorder := newTestLedger()
	scope := newTestScope()
	ledger.begin("session_1", scope)
	ctx := testCtx()
	if _, err := ledger.beforeModel(ctx, &model.LLMRequest{Model: "test-model"}); err != nil {
		t.Fatalf("beforeModel() error = %v", err)
	}
	ledger.end("session_1", nil)
	if got := len(recorder.eventsOfType(audit.EventLLMCallFailed)); got != 1 {
		t.Fatalf("failed events = %d, want 1", got)
	}
	last := recorder.lastEvent()
	if last.Status != audit.StatusFailed {
		t.Fatalf("last event = %+v, want failed", last)
	}
}

func TestLedgerDuplicateBeginRejected(t *testing.T) {
	ledger, _ := newTestLedger()
	if !ledger.begin("session_1", newTestScope()) {
		t.Fatal("first begin should succeed")
	}
	if ledger.begin("session_1", newTestScope()) {
		t.Fatal("second begin should fail")
	}
	ledger.end("session_1", nil)
	if !ledger.begin("session_1", newTestScope()) {
		t.Fatal("begin after end should succeed")
	}
}

func TestLedgerConcurrentCallbacksDoNotRace(t *testing.T) {
	ledger, recorder := newTestLedger()
	scope := newTestScope()
	ledger.begin("session_1", scope)
	ctx := testCtx()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			modelName := fmt.Sprintf("model-%d", i)
			_, _ = ledger.beforeModel(ctx, &model.LLMRequest{Model: modelName})
			_, _ = ledger.afterModel(ctx, &model.LLMResponse{ModelVersion: modelName}, nil)
		}()
	}
	wg.Wait()
	ledger.end("session_1", nil)
	if got := len(recorder.eventsOfType(audit.EventLLMCallStarted)); got != 50 {
		t.Fatalf("started events = %d, want 50", got)
	}
	if got := len(recorder.events); got != 100 {
		t.Fatalf("audit events = %d, want 100 started/completed-or-failed facts", got)
	}
}
