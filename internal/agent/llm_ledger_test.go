package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
)

type fakeAgentContext struct {
	adkagent.Context
	sessionID    string
	invocationID string
	agentName    string
}

func (c *fakeAgentContext) SessionID() string    { return c.sessionID }
func (c *fakeAgentContext) InvocationID() string { return c.invocationID }
func (c *fakeAgentContext) AgentName() string    { return c.agentName }

type stubRecorder struct {
	mu       sync.Mutex
	starts   int
	finishes int
	last     *domain.LLMCall
}

func (r *stubRecorder) Start(_ context.Context, call *domain.LLMCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts++
	r.last = call
	return nil
}

func (r *stubRecorder) Finish(_ context.Context, call *domain.LLMCall) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes++
	r.last = call
	return nil
}

func newTestLedger() (*llmLedger, *stubRecorder) {
	recorder := &stubRecorder{}
	ledger := newLLMLedger(recorder, nil, "test", func() time.Time {
		return time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	})
	return ledger, recorder
}

func newTestScope() *llmRunScope {
	return &llmRunScope{
		runID:       "run_1",
		threadID:    "thread_1",
		userID:      "user_1",
		agentName:   "chief_analyst",
		model:       "test-model",
		product:     "liki",
		graph:       "test-graph",
		contract:    "test-contract",
		prompt:      "test-prompt",
		policy:      "test-policy",
		lastByModel: make(map[string]llmCallRuntime),
	}
}

func testCtx() *fakeAgentContext {
	return &fakeAgentContext{sessionID: "session_1", invocationID: "inv_1", agentName: "chief_analyst"}
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
	if recorder.starts != 1 {
		t.Fatalf("starts = %d, want 1", recorder.starts)
	}
	response := &model.LLMResponse{ModelVersion: "test-model"}
	if _, err := ledger.afterModel(ctx, response, nil); err != nil {
		t.Fatalf("afterModel() error = %v", err)
	}
	if recorder.finishes != 1 {
		t.Fatalf("finishes = %d, want 1", recorder.finishes)
	}
	if recorder.last.Status != domain.LLMCallCompleted {
		t.Fatalf("status = %s, want completed", recorder.last.Status)
	}
	ledger.end("session_1", nil)
	if recorder.finishes != 1 {
		t.Fatalf("finishes after clean end = %d, want 1 (no double-finish)", recorder.finishes)
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
	runErr := domain.NewError("llm_timeout", "LLM timed out", nil)
	_, modelErr := ledger.onModelError(ctx, &model.LLMRequest{Model: "test-model"}, runErr)
	if modelErr == nil {
		t.Fatal("onModelError() should propagate the request error")
	}
	if recorder.last.Status != domain.LLMCallFailed {
		t.Fatalf("status = %s, want failed", recorder.last.Status)
	}
	if recorder.last.ErrorCode != "llm_timeout" {
		t.Fatalf("error_code = %s, want llm_timeout", recorder.last.ErrorCode)
	}
	ledger.end("session_1", nil)
	if recorder.finishes != 1 {
		t.Fatalf("finishes = %d, want 1", recorder.finishes)
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
	if recorder.finishes != 1 {
		t.Fatalf("finishes = %d, want 1 from failActive", recorder.finishes)
	}
	if recorder.last.Status != domain.LLMCallFailed {
		t.Fatalf("status = %s, want failed", recorder.last.Status)
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
			_, _ = ledger.beforeModel(ctx, &model.LLMRequest{Model: "test-model"})
			_, _ = ledger.afterModel(ctx, &model.LLMResponse{ModelVersion: "test-model"}, nil)
		}()
	}
	wg.Wait()
	ledger.end("session_1", nil)
	if recorder.starts < recorder.finishes {
		t.Fatalf("starts=%d must be >= finishes=%d", recorder.starts, recorder.finishes)
	}
	if recorder.starts != 50 {
		t.Fatalf("starts=%d, want 50", recorder.starts)
	}
}
