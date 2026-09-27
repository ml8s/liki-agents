package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
)

// ToolExecutionRecord is the runtime evidence for one standard ADK tool call.
// Raw arguments and results never enter audit; only canonical digests and size
// are retained.
type ToolExecutionRecord struct {
	CallID       string
	Tool         string
	RootRunID    domain.ID
	RunID        domain.ID
	ThreadID     domain.ID
	UserID       string
	AgentName    string
	AgentVersion string
	MCPServer    string
	Status       audit.Status
	StartedAt    time.Time
	FinishedAt   time.Time
	DurationMS   int64
	InputDigest  string
	InputBytes   int64
	OutputDigest string
	OutputBytes  int64
	ErrorCode    string
	ErrorMessage string
}

// ToolExecutionAuditor instruments ADK's standard tool callbacks. It records
// evidence for every LLM Agent in the deployment, including delegated agents.
type ToolExecutionAuditor struct {
	recorder audit.Recorder
	metrics  Metrics
	now      func() time.Time
	provider string

	deployment *Deployment
	agents     map[string]*AgentDefinition
	scope      func(sessionID string) (*llmRunScope, bool)

	mu      sync.Mutex
	pending map[string]ToolExecutionRecord
}

func newToolExecutionAuditor(
	recorder audit.Recorder,
	metrics Metrics,
	now func() time.Time,
	provider string,
	deployment *Deployment,
	scope func(sessionID string) (*llmRunScope, bool),
) *ToolExecutionAuditor {
	agents := make(map[string]*AgentDefinition, len(deployment.Spec.Agents))
	for index := range deployment.Spec.Agents {
		definition := &deployment.Spec.Agents[index]
		agents[definition.Name] = definition
	}
	return &ToolExecutionAuditor{
		recorder:   recorder,
		metrics:    metrics,
		now:        now,
		provider:   provider,
		deployment: deployment,
		agents:     agents,
		scope:      scope,
		pending:    make(map[string]ToolExecutionRecord),
	}
}

// BeforeTool records the started fact. Returning nil tells ADK to continue
// normal tool execution; returning args would replace the tool with its input.
func (a *ToolExecutionAuditor) BeforeTool(
	ctx adkagent.Context,
	tool tool.Tool,
	args map[string]any,
) (map[string]any, error) {
	if tool != nil && tool.Name() == adkTransferToolName {
		return nil, nil
	}
	if ctx == nil || tool == nil {
		return args, domain.NewError(domain.CodeToolCallInvalid, "tool callback context and tool are required", nil)
	}
	record, err := a.startedRecord(ctx, tool, args)
	if err != nil {
		return args, err
	}
	key := a.pendingKey(ctx, record.CallID)
	a.mu.Lock()
	a.pending[key] = record
	a.mu.Unlock()
	if auditErr := a.record(ctx, record, audit.EventToolCallStarted); auditErr != nil {
		a.mu.Lock()
		delete(a.pending, key)
		a.mu.Unlock()
		return args, auditErr
	}
	return nil, nil
}

// AfterTool records the terminal fact and preserves ADK's result and error.
func (a *ToolExecutionAuditor) AfterTool(
	ctx adkagent.Context,
	tool tool.Tool,
	args map[string]any,
	result map[string]any,
	err error,
) (map[string]any, error) {
	if tool != nil && tool.Name() == adkTransferToolName {
		return result, nil
	}
	if ctx == nil || tool == nil {
		return result, domain.NewError(domain.CodeToolCallInvalid, "tool callback context and tool are required", nil)
	}
	key := a.pendingKey(ctx, ctx.FunctionCallID())
	a.mu.Lock()
	record, ok := a.pending[key]
	a.mu.Unlock()
	if !ok {
		return result, domain.NewError(domain.CodeToolCallUnknown, "tool completion has no matching started audit", fmt.Errorf("tool %q call id %q", tool.Name(), ctx.FunctionCallID()))
	}

	finishedAt := a.now()
	record.FinishedAt = finishedAt
	record.DurationMS = finishedAt.Sub(record.StartedAt).Milliseconds()
	outputDigest, outputBytes, digestErr := audit.CanonicalDigest(result)
	if digestErr != nil {
		return result, domain.NewError(domain.CodeToolProvenanceInvalid, "digest tool result", digestErr)
	}
	record.OutputDigest = outputDigest
	record.OutputBytes = outputBytes
	if err != nil {
		record.Status = audit.StatusFailed
		record.ErrorCode = domain.CodeToolExecutionFailed
		record.ErrorMessage = "MCP tool execution failed"
	} else {
		record.Status = audit.StatusSucceeded
	}
	eventType := audit.EventToolCallCompleted
	if record.Status == audit.StatusFailed {
		eventType = audit.EventToolCallFailed
	}
	if auditErr := a.record(ctx, record, eventType); auditErr != nil {
		// Keep the started invocation pending so Runtime.Run's terminal
		// reconciliation can emit a terminal event after a transient failure.
		return result, auditErr
	}
	a.mu.Lock()
	// Another callback cannot legitimately complete this call ID first; delete
	// only after its terminal evidence has been accepted.
	delete(a.pending, key)
	a.mu.Unlock()
	if a.metrics != nil {
		a.metrics.ObserveToolCall(record.AgentName, record.Tool, string(record.Status), time.Duration(record.DurationMS)*time.Millisecond)
	}
	return result, err
}

// FailPending reconciles tool invocations that never reached AfterTool because
// the run failed, was cancelled, or its event stream terminated early.
func (a *ToolExecutionAuditor) FailPending(
	ctx context.Context,
	scope *llmRunScope,
) error {
	a.mu.Lock()
	var pending map[string]ToolExecutionRecord
	if scope == nil {
		pending = a.pending
		a.pending = make(map[string]ToolExecutionRecord)
	} else {
		pending = make(map[string]ToolExecutionRecord)
		for key, record := range a.pending {
			if string(record.RunID) == string(scope.runID) {
				pending[key] = record
				delete(a.pending, key)
			}
		}
	}
	a.mu.Unlock()

	var firstErr error
	for _, record := range pending {
		record.FinishedAt = a.now()
		record.DurationMS = record.FinishedAt.Sub(record.StartedAt).Milliseconds()
		record.Status = audit.StatusFailed
		record.ErrorCode = domain.CodeToolExecutionInterrupted
		record.ErrorMessage = "tool execution interrupted by run end"
		if auditErr := a.recordWithScope(ctx, scope, record, audit.EventToolCallFailed); auditErr != nil && firstErr == nil {
			firstErr = auditErr
		}
		if a.metrics != nil {
			a.metrics.ObserveToolCall(record.AgentName, record.Tool, string(record.Status), time.Duration(record.DurationMS)*time.Millisecond)
		}
	}
	return firstErr
}

func (a *ToolExecutionAuditor) startedRecord(
	ctx adkagent.Context,
	tool tool.Tool,
	args map[string]any,
) (ToolExecutionRecord, error) {
	scope, ok := a.scope(ctx.SessionID())
	if !ok {
		return ToolExecutionRecord{}, domain.NewError(domain.CodeAuditSessionRequired, "tool execution has no audit scope", nil)
	}
	record := ToolExecutionRecord{
		RootRunID: scope.runID,
		RunID:     scope.runID,
		ThreadID:  scope.threadID,
		UserID:    scope.userID,
	}
	definition, ok := a.agents[ctx.AgentName()]
	if !ok {
		return ToolExecutionRecord{}, domain.NewError(domain.CodeAgentDefinitionInvalid, fmt.Sprintf("tool execution agent %q is not deployed", ctx.AgentName()), nil)
	}
	reference, allowed := definition.ToolReferenceFor(tool.Name())
	if !allowed {
		return ToolExecutionRecord{}, domain.NewError(domain.CodeToolCallInvalid, fmt.Sprintf("tool %q is not allowlisted for agent %q", tool.Name(), ctx.AgentName()), nil)
	}
	inputDigest, inputBytes, err := audit.CanonicalDigest(args)
	if err != nil {
		return ToolExecutionRecord{}, domain.NewError(domain.CodeToolProvenanceInvalid, "digest tool input", err)
	}
	callID := ctx.FunctionCallID()
	if callID == "" {
		return ToolExecutionRecord{}, domain.NewError(domain.CodeToolCallIDRequired, "tool function call id is required", nil)
	}
	record.CallID = callID
	record.Tool = tool.Name()
	record.AgentName = ctx.AgentName()
	record.AgentVersion = definition.Version
	record.MCPServer = reference.Server
	record.Status = audit.StatusRunning
	record.StartedAt = a.now()
	record.InputDigest = inputDigest
	record.InputBytes = inputBytes
	return record, nil
}

func (a *ToolExecutionAuditor) record(
	ctx adkagent.Context,
	record ToolExecutionRecord,
	eventType audit.EventType,
) error {
	scope, ok := a.scope(ctx.SessionID())
	if !ok {
		return domain.NewError(domain.CodeAuditSessionRequired, "tool execution has no audit scope", nil)
	}
	definition, ok := a.agents[record.AgentName]
	if !ok {
		return domain.NewError(domain.CodeAgentDefinitionInvalid, fmt.Sprintf("tool execution agent %q is not deployed", record.AgentName), nil)
	}
	status := audit.StatusRunning
	switch eventType {
	case audit.EventToolCallCompleted:
		status = audit.StatusSucceeded
	case audit.EventToolCallFailed:
		status = audit.StatusFailed
	}
	event := audit.Event{
		ID:                    fmt.Sprintf("%s/%s/%s:%s", scope.runID, record.AgentName, record.CallID, eventType),
		SchemaVersion:         audit.SchemaV1,
		Type:                  eventType,
		OccurredAt:            record.StartedAt,
		RootRunID:             scope.runID,
		RunID:                 scope.runID,
		ThreadID:              scope.threadID,
		UserID:                scope.userID,
		Protocol:              scope.protocol,
		AgentName:             record.AgentName,
		AgentVersion:          record.AgentVersion,
		AgentDefinitionDigest: definition.Digest,
		DefinitionName:        a.deployment.Metadata.Name,
		DefinitionVersion:     a.deployment.Metadata.Version,
		DefinitionDigest:      a.deployment.Digest,
		ToolCallID:            record.CallID,
		ToolName:              record.Tool,
		Model:                 scope.model,
		Provider:              a.provider,
		Status:                status,
		TraceID:               traceIDFromContext(ctx),
		SpanID:                spanIDFromContext(ctx),
		DurationMS:            record.DurationMS,
		ErrorCode:             record.ErrorCode,
		ErrorMessage:          record.ErrorMessage,
		Payload: map[string]any{
			"mcp_server":    record.MCPServer,
			"input_digest":  record.InputDigest,
			"input_bytes":   record.InputBytes,
			"output_digest": record.OutputDigest,
			"output_bytes":  record.OutputBytes,
		},
	}
	if eventType != audit.EventToolCallStarted {
		event.OccurredAt = record.FinishedAt
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return a.recorder.Record(context.WithoutCancel(ctx), &event)
}

func (a *ToolExecutionAuditor) recordWithScope(
	ctx context.Context,
	scope *llmRunScope,
	record ToolExecutionRecord,
	eventType audit.EventType,
) error {
	if scope == nil {
		return domain.NewError(domain.CodeAuditSessionRequired, "tool audit has no run scope", nil)
	}
	definition, ok := a.agents[record.AgentName]
	if !ok {
		return domain.NewError(domain.CodeAgentDefinitionInvalid, fmt.Sprintf("tool execution agent %q is not deployed", record.AgentName), nil)
	}
	status := audit.StatusFailed
	event := audit.Event{
		ID:                    fmt.Sprintf("%s/%s/%s:%s", scope.runID, record.AgentName, record.CallID, eventType),
		SchemaVersion:         audit.SchemaV1,
		Type:                  eventType,
		OccurredAt:            record.FinishedAt,
		RootRunID:             scope.runID,
		RunID:                 scope.runID,
		ThreadID:              scope.threadID,
		UserID:                scope.userID,
		Protocol:              scope.protocol,
		AgentName:             record.AgentName,
		AgentVersion:          record.AgentVersion,
		AgentDefinitionDigest: definition.Digest,
		DefinitionName:        a.deployment.Metadata.Name,
		DefinitionVersion:     a.deployment.Metadata.Version,
		DefinitionDigest:      a.deployment.Digest,
		ToolCallID:            record.CallID,
		ToolName:              record.Tool,
		Model:                 scope.model,
		Provider:              a.provider,
		Status:                status,
		DurationMS:            record.DurationMS,
		ErrorCode:             record.ErrorCode,
		ErrorMessage:          record.ErrorMessage,
		Payload: map[string]any{
			"mcp_server":    record.MCPServer,
			"input_digest":  record.InputDigest,
			"input_bytes":   record.InputBytes,
			"output_digest": record.OutputDigest,
			"output_bytes":  record.OutputBytes,
		},
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return a.recorder.Record(context.WithoutCancel(ctx), &event)
}

func (a *ToolExecutionAuditor) pendingKey(ctx adkagent.Context, callID string) string {
	return ctx.SessionID() + "\x00" + callID
}
