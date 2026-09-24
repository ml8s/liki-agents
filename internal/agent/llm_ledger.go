package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type llmLedger struct {
	mu         sync.Mutex
	recorder   audit.Recorder
	metrics    Metrics
	now        func() time.Time
	provider   string
	deployment *Deployment
	agents     map[string]*AgentDefinition
	scopes     map[string]*llmRunScope
}

type llmRunScope struct {
	mu                sync.Mutex
	runID             domain.ID
	threadID          domain.ID
	userID            string
	agentName         string
	model             string
	graph             string
	contract          string
	instructionDigest string
	definitionName    string
	definitionVersion string
	definitionDigest  string
	nextCall          int
	lastByModel       map[string]llmCallRuntime
}

type llmCallRuntime struct {
	id        string
	model     string
	startedAt time.Time
}

func newLLMLedger(recorder audit.Recorder, metrics Metrics, provider string, deployment *Deployment, now func() time.Time) *llmLedger {
	if now == nil {
		now = time.Now
	}
	return &llmLedger{
		recorder: recorder,
		metrics:  metrics,
		provider: provider,
		now:      now,
		scopes:   make(map[string]*llmRunScope),
	}
}

func (l *llmLedger) begin(sessionID string, scope *llmRunScope) bool {
	l.mu.Lock()
	if _, exists := l.scopes[sessionID]; exists {
		l.mu.Unlock()
		return false
	}
	l.scopes[sessionID] = scope
	l.mu.Unlock()
	return true
}

func (l *llmLedger) end(sessionID string, runErr error) (bool, error) {
	l.mu.Lock()
	scope, exists := l.scopes[sessionID]
	if exists {
		delete(l.scopes, sessionID)
	}
	l.mu.Unlock()
	if exists {
		failure := runErr
		if failure == nil {
			failure = domain.NewError(domain.CodeRuntimeInterrupted, "runtime stopped before the LLM call finished", nil)
		}
		if err := l.failActive(scope, failure, l.now()); err != nil {
			return exists, err
		}
	}
	return exists, nil
}

func (l *llmLedger) scope(sessionID string) (*llmRunScope, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	scope, exists := l.scopes[sessionID]
	return scope, exists
}

func (l *llmLedger) beforeModel(ctx agent.Context, request *model.LLMRequest) (*model.LLMResponse, error) {
	scope, ok := l.scope(ctx.SessionID())
	if !ok || l.recorder == nil {
		return nil, nil
	}
	modelName := request.Model
	if modelName == "" {
		modelName = scope.model
	}
	scope.mu.Lock()
	scope.nextCall++
	callSeq := scope.nextCall
	scope.mu.Unlock()
	callID := fmt.Sprintf("%s/%s/%d", ctx.InvocationID(), modelName, callSeq)
	startedAt := l.now()
	call := &domain.LLMCall{
		ID:                callID,
		RunID:             scope.runID,
		ThreadID:          scope.threadID,
		UserID:            scope.userID,
		AgentName:         ctx.AgentName(),
		Model:             modelName,
		Provider:          l.provider,
		Status:            domain.LLMCallRunning,
		GraphVersion:      scope.graph,
		ContractVersion:   scope.contract,
		PromptVersion:     scope.instructionDigest,
		DefinitionName:    scope.definitionName,
		DefinitionVersion: scope.definitionVersion,
		DefinitionDigest:  scope.definitionDigest,
		StartedAt:         startedAt,
	}
	call.Normalize()
	l.applyAgent(call, call.AgentName)
	if err := call.Validate(); err != nil {
		return nil, err
	}
	if err := l.recordCall(ctx, call, audit.EventLLMCallStarted, audit.StatusRunning); err != nil {
		return nil, err
	}
	runtime := llmCallRuntime{id: callID, model: modelName, startedAt: startedAt}
	scope.mu.Lock()
	scope.lastByModel[modelName] = runtime
	scope.mu.Unlock()
	return nil, nil
}

func (l *llmLedger) afterModel(ctx agent.Context, response *model.LLMResponse, responseErr error) (*model.LLMResponse, error) {
	scope, ok := l.scope(ctx.SessionID())
	if !ok {
		return response, responseErr
	}
	modelName := scope.model
	if response != nil && response.ModelVersion != "" {
		modelName = response.ModelVersion
	}
	scope.mu.Lock()
	runtime, exists := scope.lastByModel[modelName]
	if !exists {
		scope.mu.Unlock()
		return response, responseErr
	}
	delete(scope.lastByModel, modelName)
	scope.mu.Unlock()

	finishedAt := l.now()
	call := &domain.LLMCall{
		ID:                runtime.id,
		RunID:             scope.runID,
		ThreadID:          scope.threadID,
		UserID:            scope.userID,
		AgentName:         ctx.AgentName(),
		Model:             modelName,
		Provider:          l.provider,
		Status:            domain.LLMCallCompleted,
		GraphVersion:      scope.graph,
		ContractVersion:   scope.contract,
		PromptVersion:     scope.instructionDigest,
		DefinitionName:    scope.definitionName,
		DefinitionVersion: scope.definitionVersion,
		DefinitionDigest:  scope.definitionDigest,
		StartedAt:         runtime.startedAt,
		FinishedAt:        finishedAt,
	}
	call.DurationMS = finishedAt.Sub(call.StartedAt).Milliseconds()
	if responseErr != nil {
		call.Fail(responseErr, finishedAt)
	} else if response != nil && response.UsageMetadata != nil {
		call.Complete(llmUsageFromResponse(response.UsageMetadata), finishedAt)
	} else {
		call.Complete(domain.LLMTokenUsage{}, finishedAt)
	}
	call.Normalize()
	l.applyAgent(call, call.AgentName)
	if err := call.Validate(); err != nil {
		return response, err
	}
	if auditErr := l.recordCall(context.WithoutCancel(ctx), call, audit.EventLLMCallCompleted, audit.StatusSucceeded); auditErr != nil {
		return response, auditErr
	}
	if l.metrics != nil {
		l.metrics.ObserveLLMCall(call.Model, string(call.Status), domain.LLMTokenUsage{
			PromptTokens:     call.PromptTokens,
			CompletionTokens: call.CompletionTokens,
			ThoughtTokens:    call.ThoughtTokens,
			TotalTokens:      call.TotalTokens,
		})
	}
	return response, responseErr
}

func (l *llmLedger) onModelError(ctx agent.Context, request *model.LLMRequest, requestErr error) (*model.LLMResponse, error) {
	scope, ok := l.scope(ctx.SessionID())
	if !ok || requestErr == nil {
		return nil, requestErr
	}
	modelName := request.Model
	if modelName == "" {
		modelName = scope.model
	}
	scope.mu.Lock()
	runtime, exists := scope.lastByModel[modelName]
	if exists {
		delete(scope.lastByModel, modelName)
		scope.mu.Unlock()
	} else {
		scope.nextCall++
		callSeq := scope.nextCall
		runtime = llmCallRuntime{
			id:        fmt.Sprintf("%s/%s/%d", ctx.InvocationID(), modelName, callSeq),
			model:     modelName,
			startedAt: l.now(),
		}
		scope.mu.Unlock()
	}
	finishedAt := l.now()
	call := &domain.LLMCall{
		ID:                runtime.id,
		RunID:             scope.runID,
		ThreadID:          scope.threadID,
		UserID:            scope.userID,
		AgentName:         ctx.AgentName(),
		Model:             modelName,
		Provider:          l.provider,
		Status:            domain.LLMCallFailed,
		GraphVersion:      scope.graph,
		ContractVersion:   scope.contract,
		PromptVersion:     scope.instructionDigest,
		DefinitionName:    scope.definitionName,
		DefinitionVersion: scope.definitionVersion,
		DefinitionDigest:  scope.definitionDigest,
		StartedAt:         runtime.startedAt,
		FinishedAt:        finishedAt,
	}
	call.DurationMS = finishedAt.Sub(call.StartedAt).Milliseconds()
	call.Fail(requestErr, finishedAt)
	call.Normalize()
	l.applyAgent(call, call.AgentName)
	if err := call.Validate(); err != nil {
		return nil, err
	}
	if auditErr := l.recordCall(context.WithoutCancel(ctx), call, audit.EventLLMCallFailed, audit.StatusFailed); auditErr != nil {
		return nil, auditErr
	}
	if l.metrics != nil {
		l.metrics.ObserveLLMCall(call.Model, string(call.Status), domain.LLMTokenUsage{
			PromptTokens:     call.PromptTokens,
			CompletionTokens: call.CompletionTokens,
			ThoughtTokens:    call.ThoughtTokens,
			TotalTokens:      call.TotalTokens,
		})
	}
	return nil, requestErr
}

func (l *llmLedger) failActive(scope *llmRunScope, err error, finishedAt time.Time) error {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	var firstErr error
	for _, runtime := range scope.lastByModel {
		call := &domain.LLMCall{
			ID: runtime.id, RunID: scope.runID, ThreadID: scope.threadID,
			UserID: scope.userID, AgentName: scope.agentName, Model: runtime.model,
			Provider: l.provider, Status: domain.LLMCallFailed,
			GraphVersion: scope.graph, ContractVersion: scope.contract,
			PromptVersion: scope.instructionDigest,
			StartedAt:     runtime.startedAt, FinishedAt: finishedAt,
		}
		call.DurationMS = finishedAt.Sub(call.StartedAt).Milliseconds()
		call.Fail(err, finishedAt)
		l.applyAgent(call, scope.agentName)
		if recordErr := l.recordCall(context.Background(), call, audit.EventLLMCallFailed, audit.StatusFailed); recordErr != nil && firstErr == nil {
			firstErr = recordErr
		}
	}
	scope.lastByModel = make(map[string]llmCallRuntime)
	return firstErr
}

func (l *llmLedger) applyAgent(call *domain.LLMCall, name string) {
	definition, ok := l.agents[name]
	if !ok {
		return
	}
	call.AgentName = definition.Name
	call.AgentVersion = definition.Version
	call.AgentDefinitionDigest = definition.Digest
}

func (l *llmLedger) recordCall(ctx context.Context, call *domain.LLMCall, eventType audit.EventType, status audit.Status) error {
	if l.recorder == nil {
		return domain.NewError(domain.CodeAuditRecorderMissing, "audit recorder is required", nil)
	}
	event := audit.Event{
		ID:                    call.ID + ":" + string(eventType),
		SchemaVersion:         audit.SchemaV1,
		Type:                  eventType,
		RootRunID:             call.RunID,
		RunID:                 call.RunID,
		ThreadID:              call.ThreadID,
		UserID:                call.UserID,
		AgentName:             call.AgentName,
		AgentVersion:          call.AgentVersion,
		AgentDefinitionDigest: call.AgentDefinitionDigest,
		Model:                 call.Model,
		Provider:              call.Provider,
		Status:                status,
		DurationMS:            call.DurationMS,
		ErrorCode:             call.ErrorCode,
		ErrorMessage:          call.ErrorMessage,
	}
	if eventType == audit.EventLLMCallStarted {
		event.OccurredAt = call.StartedAt
	} else {
		event.OccurredAt = call.FinishedAt
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return l.recorder.Record(ctx, &event)
}

func llmUsageFromResponse(usage *genai.GenerateContentResponseUsageMetadata) domain.LLMTokenUsage {
	if usage == nil {
		return domain.LLMTokenUsage{}
	}
	return domain.LLMTokenUsage{
		PromptTokens:     int64(usage.PromptTokenCount),
		CompletionTokens: int64(usage.CandidatesTokenCount),
		ThoughtTokens:    int64(usage.ThoughtsTokenCount),
		TotalTokens:      int64(usage.TotalTokenCount),
	}
}
