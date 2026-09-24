package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type stubMetrics struct {
	calls []string
}

func (m *stubMetrics) ObserveLLMCall(string, string, domain.LLMTokenUsage) {}

func (m *stubMetrics) ObserveAgentDelegation(caller, target, status string, duration time.Duration) {
	m.calls = append(m.calls, caller+"/"+target+"/"+status)
}

func (m *stubMetrics) ObserveToolCall(agent, tool, status string, duration time.Duration) {
	m.calls = append(m.calls, agent+"/"+tool+"/"+status)
}

type fakeTool string

func (t fakeTool) Name() string        { return string(t) }
func (t fakeTool) Description() string { return "generic test tool" }
func (t fakeTool) IsLongRunning() bool { return false }

func newToolAuditor(t *testing.T) (*ToolExecutionAuditor, *recordingAudit, *stubMetrics, *fakeAgentContext) {
	t.Helper()
	deployment := NewTestDeployment(t)
	events := &recordingAudit{}
	metrics := &stubMetrics{}
	ledger := newLLMLedger(events, metrics, "test-provider", deployment, func() time.Time {
		return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	})
	auditor := newToolExecutionAuditor(
		events,
		metrics,
		func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) },
		"test-provider",
		deployment,
		ledger.scope,
	)
	ledger.begin("session_1", newTestScope())

	traceProvider := sdktrace.NewTracerProvider()
	tracer := traceProvider.Tracer("tool-audit-test")
	traceCtx, span := tracer.Start(context.Background(), "tool audit test")
	t.Cleanup(func() {
		_ = traceProvider.Shutdown(context.Background())
		span.End()
	})

	ctx := &fakeAgentContext{
		sessionID:      "session_1",
		invocationID:   "inv_1",
		agentName:      "main",
		functionCallID: "call_1",
		traceContext:   traceCtx,
	}
	return auditor, events, metrics, ctx
}

func TestToolAuditorRecordsSuccessfulCall(t *testing.T) {
	auditor, events, metrics, ctx := newToolAuditor(t)
	args := map[string]any{"question": "career"}
	if _, err := auditor.BeforeTool(ctx, fakeTool("test_tool"), args); err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}
	if _, err := auditor.AfterTool(ctx, fakeTool("test_tool"), args, map[string]any{"answer": "ok"}, nil); err != nil {
		t.Fatalf("AfterTool() error = %v", err)
	}
	started := events.eventsOfType(audit.EventToolCallStarted)
	completed := events.eventsOfType(audit.EventToolCallCompleted)
	if len(started) != 1 || len(completed) != 1 {
		t.Fatalf("tool audit events = %d started / %d completed, want 1/1", len(started), len(completed))
	}
	if started[0].ToolCallID != "call_1" || started[0].Status != audit.StatusRunning {
		t.Fatalf("started event = %+v", started[0])
	}
	if started[0].TraceID == "" || started[0].SpanID == "" {
		t.Fatal("started audit event lacks trace correlation")
	}
	if completed[0].Status != audit.StatusSucceeded || completed[0].DurationMS != 0 {
		t.Fatalf("completed event = %+v", completed[0])
	}
	if got := completed[0].Payload["input_digest"]; !strings.HasPrefix(got.(string), "sha256:") {
		t.Fatalf("input digest = %#v, want sha256 digest", got)
	}
	if got := completed[0].Payload["output_bytes"]; got.(int64) == 0 {
		t.Fatalf("output bytes = %#v, want non-zero", got)
	}
	if len(metrics.calls) != 1 || metrics.calls[0] != "main/test_tool/succeeded" {
		t.Fatalf("tool metrics = %v", metrics.calls)
	}
}

func TestToolAuditorRecordsFailedCall(t *testing.T) {
	auditor, events, metrics, ctx := newToolAuditor(t)
	if _, err := auditor.BeforeTool(ctx, fakeTool("test_tool"), nil); err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}
	toolErr := errors.New("tool failed")
	result, auditErr := auditor.AfterTool(ctx, fakeTool("test_tool"), nil, nil, toolErr)
	if !errors.Is(auditErr, toolErr) || result != nil {
		t.Fatalf("AfterTool() = %#v, %v, want original error", result, auditErr)
	}
	failed := events.eventsOfType(audit.EventToolCallFailed)
	if len(failed) != 1 {
		t.Fatalf("failed events = %d, want 1", len(failed))
	}
	if failed[0].ErrorCode != domain.CodeToolExecutionFailed {
		t.Fatalf("error code = %s", failed[0].ErrorCode)
	}
	if failed[0].TraceID == "" || failed[0].SpanID == "" {
		t.Fatal("failed audit event lacks trace correlation")
	}
	if len(metrics.calls) != 1 || !strings.HasSuffix(metrics.calls[0], "/failed") {
		t.Fatalf("tool metrics = %v", metrics.calls)
	}
}

func TestToolAuditorFailsPendingWhenRunEnds(t *testing.T) {
	auditor, events, metrics, ctx := newToolAuditor(t)
	if _, err := auditor.BeforeTool(ctx, fakeTool("test_tool"), map[string]any{"input": "value"}); err != nil {
		t.Fatalf("BeforeTool() error = %v", err)
	}

	cause := errors.New("run terminated")
	if err := auditor.FailPending(context.Background(), newTestScope(), cause); err != nil {
		t.Fatalf("FailPending() error = %v", err)
	}
	if err := auditor.FailPending(context.Background(), newTestScope(), cause); err != nil {
		t.Fatalf("second FailPending() error = %v", err)
	}

	failed := events.eventsOfType(audit.EventToolCallFailed)
	if len(failed) != 1 {
		t.Fatalf("failed events = %d, want 1", len(failed))
	}
	if failed[0].ErrorCode != domain.CodeToolExecutionInterrupted {
		t.Fatalf("error code = %s, want %s", failed[0].ErrorCode, domain.CodeToolExecutionInterrupted)
	}
	if failed[0].ErrorMessage != cause.Error() {
		t.Fatalf("error message = %q, want %q", failed[0].ErrorMessage, cause.Error())
	}
	if failed[0].Payload["input_digest"].(string) == "" {
		t.Fatal("interrupted tool audit lacks input digest")
	}
	if len(metrics.calls) != 1 || !strings.HasSuffix(metrics.calls[0], "/failed") {
		t.Fatalf("tool metrics = %v", metrics.calls)
	}
}

func trackerNames(tracker *toolNameTracker) []string {
	return tracker.Names()
}
