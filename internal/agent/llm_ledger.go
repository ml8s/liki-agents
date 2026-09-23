package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

type llmLedger struct {
	mu       sync.Mutex
	recorder LLMCallRecorder
	metrics  Metrics
	now      func() time.Time
	provider string
	scopes   map[string]*llmRunScope
}

type llmRunScope struct {
	mu          sync.Mutex
	runID       domain.ID
	threadID    domain.ID
	userID      string
	agentName   string
	model       string
	product     string
	graph       string
	contract    string
	prompt      string
	policy      string
	nextCall    int
	lastByModel map[string]llmCallRuntime
}

type llmCallRuntime struct {
	id        string
	model     string
	startedAt time.Time
}

func newLLMLedger(recorder LLMCallRecorder, metrics Metrics, provider string, now func() time.Time) *llmLedger {
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

func (l *llmLedger) end(sessionID string, runErr error) {
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
		l.failActive(scope, failure, l.now())
	}
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
		ID:              callID,
		RunID:           scope.runID,
		ThreadID:        scope.threadID,
		UserID:          scope.userID,
		Product:         scope.product,
		AgentName:       ctx.AgentName(),
		Model:           modelName,
		Provider:        l.provider,
		Status:          domain.LLMCallRunning,
		GraphVersion:    scope.graph,
		ContractVersion: scope.contract,
		PromptVersion:   scope.prompt,
		PolicyVersion:   scope.policy,
		StartedAt:       startedAt,
	}
	call.Normalize()
	if err := call.Validate(); err != nil {
		return nil, err
	}
	if err := l.recorder.Start(ctx, call); err != nil {
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
		ID:              runtime.id,
		RunID:           scope.runID,
		ThreadID:        scope.threadID,
		UserID:          scope.userID,
		Product:         scope.product,
		AgentName:       ctx.AgentName(),
		Model:           modelName,
		Provider:        l.provider,
		Status:          domain.LLMCallCompleted,
		GraphVersion:    scope.graph,
		ContractVersion: scope.contract,
		PromptVersion:   scope.prompt,
		PolicyVersion:   scope.policy,
		StartedAt:       runtime.startedAt,
		FinishedAt:      finishedAt,
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
	if err := call.Validate(); err != nil {
		return response, err
	}
	if err := l.recorder.Finish(ctx, call); err != nil {
		return response, err
	}
	if l.metrics != nil {
		l.metrics.ObserveLLMCall(call.Model, string(call.Status), call.PromptTokens, call.CompletionTokens, call.TotalTokens)
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
		ID:              runtime.id,
		RunID:           scope.runID,
		ThreadID:        scope.threadID,
		UserID:          scope.userID,
		Product:         scope.product,
		AgentName:       ctx.AgentName(),
		Model:           modelName,
		Provider:        l.provider,
		Status:          domain.LLMCallFailed,
		GraphVersion:    scope.graph,
		ContractVersion: scope.contract,
		PromptVersion:   scope.prompt,
		PolicyVersion:   scope.policy,
		StartedAt:       runtime.startedAt,
		FinishedAt:      finishedAt,
	}
	call.DurationMS = finishedAt.Sub(call.StartedAt).Milliseconds()
	call.Fail(requestErr, finishedAt)
	call.Normalize()
	if err := call.Validate(); err != nil {
		return nil, err
	}
	if err := l.recorder.Finish(ctx, call); err != nil {
		return nil, err
	}
	if l.metrics != nil {
		l.metrics.ObserveLLMCall(call.Model, string(call.Status), call.PromptTokens, call.CompletionTokens, call.TotalTokens)
	}
	return nil, requestErr
}

func (l *llmLedger) failActive(scope *llmRunScope, err error, finishedAt time.Time) {
	scope.mu.Lock()
	defer scope.mu.Unlock()
	for _, runtime := range scope.lastByModel {
		call := &domain.LLMCall{
			ID: runtime.id, RunID: scope.runID, ThreadID: scope.threadID,
			UserID: scope.userID, AgentName: scope.agentName, Model: runtime.model,
			Product:  scope.product,
			Provider: l.provider, Status: domain.LLMCallFailed,
			GraphVersion: scope.graph, ContractVersion: scope.contract,
			PromptVersion: scope.prompt, PolicyVersion: scope.policy,
			StartedAt: runtime.startedAt, FinishedAt: finishedAt,
		}
		call.DurationMS = finishedAt.Sub(call.StartedAt).Milliseconds()
		call.Fail(err, finishedAt)
		_ = l.recorder.Finish(context.Background(), call)
	}
	scope.lastByModel = make(map[string]llmCallRuntime)
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
