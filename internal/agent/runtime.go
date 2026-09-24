// Package agent owns the single ADK execution graph and its external model and
// Engine MCP dependencies. Protocol packages adapt this runtime; they never
// create a second execution path.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/plugin/retryandreflect"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
)

const (
	// structuredOutputStateKey matches llmagent.Config.OutputKey. ADK parses
	// the model reply against OutputSchema, clears Event.Output before
	// yielding it, and persists the parsed value into session state under
	// this key. Session state is the framework contract for consuming it.
	structuredOutputStateKey = "structured_analysis"
)

type Runtime struct {
	config            Config
	definition        *Deployment
	entrypoint        *AgentDefinition
	root              agent.Agent
	sessions          session.Service
	runner            *runner.Runner
	runnerConfig      runner.Config
	llm               *llmLedger
	delegationAuditor *AgentReferenceAuditor
	toolAuditor       *ToolExecutionAuditor
	tracer            trace.Tracer
}

func NewRuntime(config Config) (*Runtime, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.AppName == "" {
		config.AppName = "liki-agents"
	}
	if config.Model == "" {
		return nil, domain.NewError(domain.CodeLLMModelMissing, "LLM model is required", domain.ErrInvalidInput)
	}
	if config.ModelTimeout <= 0 {
		config.ModelTimeout = 120 * time.Second
	}
	if config.EngineMCPURL == "" {
		return nil, domain.NewError(domain.CodeEngineMCPURLMissing, "Engine MCP URL is required", domain.ErrInvalidInput)
	}
	if config.AuditRecorder == nil {
		return nil, domain.NewError(domain.CodeAuditRecorderMissing, "audit recorder is required", domain.ErrInvalidInput)
	}
	if config.Deployment == nil {
		return nil, domain.NewError(domain.CodeAgentDefinitionMissing, "AgentDefinition is required", domain.ErrInvalidInput)
	}
	entrypoint, err := config.Deployment.EntrypointDefinition()
	if err != nil {
		return nil, domain.NewError(domain.CodeAgentDefinitionInvalid, "select entrypoint AgentDefinition", err)
	}
	switch config.StructuredOutput {
	case StructuredOutputNone, StructuredOutputJSONSchema, StructuredOutputJSONObject:
	default:
		return nil, domain.NewError(domain.CodeStructuredOutputCapabilityInvalid, "structured output capability is invalid", domain.ErrInvalidInput)
	}
	structuredAgents := 0
	for _, candidate := range config.Deployment.Spec.Agents {
		if candidate.Output.Structured() {
			structuredAgents++
		}
	}
	if structuredAgents > 0 && config.StructuredOutput == StructuredOutputNone ||
		structuredAgents == 0 && config.StructuredOutput != StructuredOutputNone {
		return nil, domain.NewError(domain.CodeStructuredOutputCapabilityInvalid, "structured output capability does not match AgentDeployment", domain.ErrInvalidInput)
	}
	if config.EngineTimeout <= 0 {
		config.EngineTimeout = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.TracerProvider == nil {
		config.TracerProvider = otel.GetTracerProvider()
	}
	tracer := config.TracerProvider.Tracer("github.com/ml8s/liki-agents/agent")
	// Protocol callers own durable conversation/product history. Each ADK
	// session is deliberately run-scoped working state and must not become a
	// second durable conversation store.
	sessionService := session.InMemoryService()

	var aiModel model.LLM
	if config.modelOverride != nil {
		aiModel = config.modelOverride
	} else {
		var err error
		// Use ADK's official OpenAI-compatible model. The provider-aware
		// structured-output adapter handles providers without JSON schema mode.
		aiModel, err = openaimodel.NewModel(context.Background(), config.Model, &openaimodel.ClientConfig{
			APIKey:     config.ModelAPIKey,
			BaseURL:    config.ModelBaseURL,
			HTTPClient: &http.Client{Timeout: config.ModelTimeout},
		})
		if err != nil {
			return nil, domain.NewError(domain.CodeLLMUnavailable, "create LLM model", err)
		}
		useJSONObject := config.StructuredOutput == StructuredOutputJSONObject
		if useJSONObject {
			compatModel, err := newJSONObjectModel(aiModel)
			if err != nil {
				return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create provider structured output model", err)
			}
			aiModel = compatModel
		}
	}
	temperature := float32(config.Temperature)
	ledger := newLLMLedger(config.AuditRecorder, config.Metrics, config.Provider, config.Deployment, config.Now)
	engineTools, err := newEngineToolset(config)
	if err != nil {
		return nil, err
	}
	toolAuditor := newToolExecutionAuditor(
		config.AuditRecorder,
		config.Metrics,
		config.Now,
		config.Provider,
		config.Deployment,
		ledger.scope,
	)
	delegationAuditor := newAgentReferenceAuditor(
		config.AuditRecorder,
		config.Metrics,
		config.Now,
		config.Deployment,
		entrypoint.Name,
		ledger.scope,
	)
	builtAgents := make(map[string]agent.Agent, len(config.Deployment.Spec.Agents))
	building := make(map[string]struct{})
	var buildAgent func(*AgentDefinition) (agent.Agent, error)
	buildAgent = func(definition *AgentDefinition) (agent.Agent, error) {
		if built, ok := builtAgents[definition.Name]; ok {
			return built, nil
		}
		if _, active := building[definition.Name]; active {
			return nil, domain.NewError(domain.CodeAgentDefinitionInvalid, fmt.Sprintf("agent delegation cycle through %q", definition.Name), nil)
		}
		building[definition.Name] = struct{}{}
		outputKey := ""
		if definition.Output.Structured() {
			outputKey = structuredOutputStateKey
		}
		subAgents := make([]agent.Agent, 0, len(definition.SubAgents))
		for _, reference := range definition.SubAgents {
			target, ok := config.Deployment.Agent(reference.Name)
			if !ok {
				return nil, domain.NewError(domain.CodeAgentDefinitionInvalid, fmt.Sprintf("sub-agent %q is not defined", reference.Name), nil)
			}
			subAgent, err := buildAgent(target)
			if err != nil {
				return nil, err
			}
			subAgents = append(subAgents, subAgent)
		}
		toolset := tool.FilterToolset(engineTools, tool.AllowedToolsPredicate(definition.Tools.Allow))
		delete(building, definition.Name)
		built, err := llmagent.New(definition.ADKConfig(ADKAgentRuntime{
			RawOutputSchema: definition.RawOutputSchema,
			Model:           aiModel,
			Toolset:         toolset,
			SubAgents:       subAgents,
			Temperature:     temperature,
			OutputKey:       outputKey,
			IsEntrypoint:    definition.Name == entrypoint.Name,
			BeforeModelCallbacks: []llmagent.BeforeModelCallback{
				ledger.beforeModel,
			},
			AfterModelCallbacks: []llmagent.AfterModelCallback{
				ledger.afterModel,
			},
			OnModelErrorCallbacks: []llmagent.OnModelErrorCallback{
				ledger.onModelError,
			},
			BeforeAgentCallbacks: []agent.BeforeAgentCallback{
				delegationAuditor.BeforeAgent,
			},
			AfterAgentCallbacks: []agent.AfterAgentCallback{
				delegationAuditor.AfterAgent,
			},
			BeforeToolCallbacks: []llmagent.BeforeToolCallback{
				toolAuditor.BeforeTool,
			},
			AfterToolCallbacks: []llmagent.AfterToolCallback{
				toolAuditor.AfterTool,
			},
		}))
		if err != nil {
			return nil, domain.NewError(domain.CodeRuntimeInitFailed, fmt.Sprintf("create ADK agent %q", definition.Name), err)
		}
		builtAgents[definition.Name] = built
		return built, nil
	}
	retryPlugin, err := retryandreflect.New(
		retryandreflect.WithMaxRetries(2),
		retryandreflect.WithErrorIfRetryExceeded(true),
	)
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create tool retry plugin", err)
	}
	plugins := []*plugin.Plugin{retryPlugin}
	root, err := buildAgent(entrypoint)
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create ADK agent", err)
	}
	runnerConfig := runner.Config{
		AppName:           config.AppName,
		Agent:             root,
		SessionService:    sessionService,
		AutoCreateSession: true,
		PluginConfig: runner.PluginConfig{
			Plugins:      plugins,
			CloseTimeout: 5 * time.Second,
		},
	}
	runnerInstance, err := runner.New(runnerConfig)
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create ADK runner", err)
	}
	return &Runtime{
		config:            config,
		root:              root,
		sessions:          sessionService,
		runner:            runnerInstance,
		runnerConfig:      runnerConfig,
		llm:               ledger,
		delegationAuditor: delegationAuditor,
		toolAuditor:       toolAuditor,
		tracer:            tracer,
		definition:        config.Deployment,
		entrypoint:        entrypoint,
	}, nil
}

func newEngineToolset(config Config) (tool.Toolset, error) {
	var credential auth.CredentialProvider
	if config.EngineToken != "" {
		credential = auth.StaticToken(config.EngineToken)
	}
	engineTools, err := mcptoolset.New(mcptoolset.Config{
		Transport: newEngineToolTransport(config),
		Auth:      credential,
	})
	if err != nil {
		return nil, domain.NewError(domain.CodeEngineToolsUnavailable, "create Engine MCP toolset", err)
	}
	return engineTools, nil
}

func agentMode(mode AgentMode) llmagent.Mode {
	switch mode {
	case AgentModeTask:
		return llmagent.ModeTask
	case AgentModeSingleTurn:
		return llmagent.ModeSingleTurn
	default:
		return llmagent.ModeChat
	}
}

// RootAgent exposes the single ADK graph to standard protocol adapters. ADK
// types do not cross further inward than this runtime component.
func (r *Runtime) RootAgent() agent.Agent {
	return r.root
}

// RunnerConfig returns the shared ADK runtime configuration used by protocol
// bindings. The same plugins and run-scoped session service are preserved.
func (r *Runtime) RunnerConfig() runner.Config {
	return r.runnerConfig
}

// AuditRunScope identifies an externally driven ADK execution for the LLM
// audit ledger. It does not create a second execution path.
type AuditRunScope struct {
	RunID    string
	ThreadID string
	UserID   string
}

// BeginAuditRun attaches protocol identity to model callbacks when an official
// protocol executor drives the shared ADK runtime directly.
func (r *Runtime) BeginAuditRun(ctx context.Context, sessionID string, scope AuditRunScope) error {
	scope.RunID = strings.TrimSpace(scope.RunID)
	scope.ThreadID = strings.TrimSpace(scope.ThreadID)
	scope.UserID = strings.TrimSpace(scope.UserID)
	if scope.RunID == "" || scope.ThreadID == "" || scope.UserID == "" {
		return domain.NewError(domain.CodeAuditScopeRequired, "run, thread, and user identifiers are required", domain.ErrInvalidInput)
	}
	if sessionID == "" {
		return domain.NewError(domain.CodeAuditSessionRequired, "audit session identifier is required", domain.ErrInvalidInput)
	}
	started := r.llm.begin(sessionID, &llmRunScope{
		runID:             domain.ID(scope.RunID),
		threadID:          domain.ID(scope.ThreadID),
		userID:            scope.UserID,
		agentName:         r.entrypoint.Name,
		model:             r.config.Model,
		graph:             r.config.GraphVersion,
		contract:          r.config.ContractVersion,
		instructionDigest: r.entrypoint.InstructionDigest,
		definitionName:    r.definition.Metadata.Name,
		definitionVersion: r.definition.Metadata.Version,
		definitionDigest:  r.definition.Digest,
		lastByModel:       make(map[string]llmCallRuntime),
	})
	if !started {
		return domain.NewError(domain.CodeAuditSessionActive, "an audit run is already active for this session", domain.ErrInvalidInput)
	}
	return r.config.AuditRecorder.Record(ctx, &audit.Event{
		ID:            scope.RunID + ":started",
		SchemaVersion: audit.SchemaV1,
		Type:          audit.EventRunStarted,
		OccurredAt:    r.config.Now(),
		RootRunID:     domain.ID(scope.RunID),
		RunID:         domain.ID(scope.RunID),
		ThreadID:      domain.ID(scope.ThreadID),
		UserID:        scope.UserID,
		AgentName:     r.entrypoint.Name,
		Status:        audit.StatusRunning,
	})
}

// EndAuditRun completes an externally driven audit lifecycle exactly once.
func (r *Runtime) EndAuditRun(ctx context.Context, sessionID string, runErr error) error {
	ctx = context.WithoutCancel(ctx)
	if sessionID == "" {
		return nil
	}
	scope, existed := r.llm.scope(sessionID)
	_, ledgerErr := r.llm.end(sessionID, runtimeError(runErr))
	if ledgerErr != nil {
		return ledgerErr
	}
	if existed {
		if auditErr := r.delegationAuditor.FailPending(ctx, scope, runErr); auditErr != nil {
			return auditErr
		}
		if auditErr := r.toolAuditor.FailPending(ctx, scope, runErr); auditErr != nil {
			return auditErr
		}
	}
	if !existed {
		return nil
	}
	eventType := audit.EventRunCompleted
	status := audit.StatusSucceeded
	if runErr != nil {
		eventType = audit.EventRunFailed
		status = audit.StatusFailed
	}
	event := audit.Event{
		ID:            sessionID + ":" + string(eventType),
		SchemaVersion: audit.SchemaV1,
		Type:          eventType,
		OccurredAt:    r.config.Now(),
		RootRunID:     scope.runID,
		RunID:         scope.runID,
		ThreadID:      scope.threadID,
		UserID:        scope.userID,
		AgentName:     scope.agentName,
		Status:        status,
	}
	if runErr != nil {
		runtimeErr := runtimeError(runErr)
		var domainErr *domain.Error
		if errors.As(runtimeErr, &domainErr) {
			event.ErrorCode = domainErr.Code
			event.ErrorMessage = domainErr.Message
		} else {
			event.ErrorCode = domain.CodeRuntimeFailed
			event.ErrorMessage = runtimeErr.Error()
		}
	}
	return r.config.AuditRecorder.Record(ctx, &event)
}

// Run executes the shared ADK runtime and exposes native ADK events to a
// protocol adapter. It is not a public API and creates no second business path.
func (r *Runtime) Run(
	ctx context.Context,
	request RunRequest,
	observe func(*session.Event) error,
) (RunResult, error) {
	var runErr error
	ctx, runSpan := r.tracer.Start(
		ctx,
		"agent.run "+r.entrypoint.Name,
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(
			attribute.String("liki.run.id", request.RunID),
			attribute.String("liki.run.root_id", request.RunID),
			attribute.String("liki.thread.id", request.ThreadID),
			attribute.String("gen_ai.agent.name", r.entrypoint.Name),
			attribute.String("liki.agent.version", r.entrypoint.Version),
			attribute.String("liki.definition.name", r.definition.Metadata.Name),
			attribute.String("liki.definition.version", r.definition.Metadata.Version),
			attribute.String("liki.definition.digest", r.definition.Digest),
		),
	)
	defer runSpan.End()
	startedAt := r.config.Now()
	state := &runState{
		capturePlain:   !r.entrypoint.Output.Structured(),
		plainAgentName: r.entrypoint.Name,
	}
	projector := newEventProjector(r.definition)
	// Session identity is run-scoped. Thread identity remains owned by the
	// application database, preventing implicit duplication of durable history.
	sessionID := "run:" + request.RunID
	scope := &llmRunScope{
		runID:             domain.ID(request.RunID),
		threadID:          domain.ID(request.ThreadID),
		userID:            request.UserID,
		agentName:         r.entrypoint.Name,
		model:             r.config.Model,
		graph:             r.config.GraphVersion,
		contract:          r.config.ContractVersion,
		instructionDigest: r.entrypoint.InstructionDigest,
		definitionName:    r.definition.Metadata.Name,
		definitionVersion: r.definition.Metadata.Version,
		definitionDigest:  r.definition.Digest,
		lastByModel:       make(map[string]llmCallRuntime),
	}
	r.llm.begin(sessionID, scope)
	if auditErr := r.recordRunAudit(ctx, request, audit.EventRunStarted, audit.StatusRunning, startedAt, nil); auditErr != nil {
		return RunResult{}, auditErr
	}
	defer func() {
		// Capture the run scope before the LLM ledger removes it so pending
		// delegation/tool auditors can still emit terminal evidence.
		auditScope, _ := r.llm.scope(sessionID)
		_, _ = r.llm.end(sessionID, runErr)
		if delegationAuditErr := r.delegationAuditor.FailPending(context.WithoutCancel(ctx), auditScope, runErr); delegationAuditErr != nil && runErr == nil {
			runErr = delegationAuditErr
		}
		if toolAuditErr := r.toolAuditor.FailPending(context.WithoutCancel(ctx), auditScope, runErr); toolAuditErr != nil && runErr == nil {
			runErr = toolAuditErr
		}
		eventType := audit.EventRunCompleted
		status := audit.StatusSucceeded
		var auditFailure error
		if runErr != nil {
			eventType = audit.EventRunFailed
			status = audit.StatusFailed
			auditFailure = runErr
		}
		auditCtx := context.WithoutCancel(ctx)
		if auditErr := r.recordRunAudit(auditCtx, request, eventType, status, startedAt, auditFailure); auditErr != nil && runErr == nil {
			runErr = auditErr
		}
	}()
	r.config.Logger.InfoContext(ctx, "agent_run_started",
		append(traceLogFields(ctx),
			"run_id", request.RunID,
			"thread_id", request.ThreadID,
			"user_id", request.UserID,
			"model", r.config.Model,
		)...)

	events := r.runner.Run(
		ctx,
		request.UserID,
		sessionID,
		buildUserContent(request),
		agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
	)
	for event, err := range events {
		if err != nil {
			runSpan.RecordError(err)
			runSpan.SetStatus(codes.Error, runtimeError(err).Error())
			r.config.Logger.WarnContext(ctx, "agent_run_failed",
				append(traceLogFields(ctx),
					"run_id", request.RunID, "error", err,
				)...)
			runErr = runtimeError(err)
			return RunResult{}, runErr
		}
		state.consume(event)
		if observe != nil {
			if visible, visibleOK := projector.Project(event); visibleOK {
				if err := observe(visible); err != nil {
					runSpan.RecordError(err)
					runSpan.SetStatus(codes.Error, runtimeError(err).Error())
					runErr = runtimeError(err)
					return RunResult{}, runErr
				}
			}
		}
	}
	if r.entrypoint.Output.Structured() && state.output.JSON == nil {
		if err := r.loadStructuredAnalysis(ctx, request, sessionID, state); err != nil {
			runSpan.RecordError(err)
			runSpan.SetStatus(codes.Error, runtimeError(err).Error())
			runErr = runtimeError(err)
			return RunResult{}, runErr
		}
	}

	if strings.TrimSpace(state.output.Text) == "" {
		r.config.Logger.Warn("agent_run_empty_response", "run_id", request.RunID)
		runErr = domain.NewError(domain.CodeRuntimeEmptyResponse, "ADK runtime returned no answer text", nil)
		return RunResult{}, runErr
	}
	runSpan.SetStatus(codes.Ok, "")
	r.config.Logger.InfoContext(ctx, "agent_run_completed",
		append(traceLogFields(ctx),
			"run_id", request.RunID,
			"agent", r.entrypoint.Name,
			"tools_used", state.tools.Names(),
			"model", r.config.Model,
		)...)

	return RunResult{
		Definition: r.definition.DefinitionRef(),
		Output:     state.output.JSON,
		Text:       state.output.Text,
	}, nil
}

func (r *Runtime) recordRunAudit(
	ctx context.Context,
	request RunRequest,
	eventType audit.EventType,
	status audit.Status,
	startedAt time.Time,
	cause error,
) error {
	event := audit.Event{
		TraceID:           traceIDFromContext(ctx),
		SpanID:            spanIDFromContext(ctx),
		DefinitionName:    r.definition.Metadata.Name,
		DefinitionVersion: r.definition.Metadata.Version,
		DefinitionDigest:  r.definition.Digest,
		ID:                request.RunID + ":" + string(eventType),
		SchemaVersion:     audit.SchemaV1,
		Type:              eventType,
		OccurredAt:        r.config.Now(),
		RootRunID:         domain.ID(request.RunID),
		RunID:             domain.ID(request.RunID),
		ThreadID:          domain.ID(request.ThreadID),
		UserID:            request.UserID,
		AgentName:         r.entrypoint.Name,
		Model:             r.config.Model,
		Provider:          r.config.Provider,
		Status:            status,
		DurationMS:        r.config.Now().Sub(startedAt).Milliseconds(),
	}
	if cause != nil {
		runtimeErr := runtimeError(cause)
		var domainErr *domain.Error
		if errors.As(runtimeErr, &domainErr) {
			event.ErrorCode = domainErr.Code
			event.ErrorMessage = domainErr.Message
		} else {
			event.ErrorCode = domain.CodeRuntimeFailed
			event.ErrorMessage = runtimeErr.Error()
		}
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return r.config.AuditRecorder.Record(ctx, &event)
}

// loadStructuredAnalysis consumes the parsed model output from ADK session
// state. Event.Output is deliberately cleared by ADK Runner before yielding,
// so session state is the only framework-supported source.
func (r *Runtime) loadStructuredAnalysis(ctx context.Context, request RunRequest, sessionID string, state *runState) error {
	response, err := r.sessions.Get(ctx, &session.GetRequest{
		AppName:   r.config.AppName,
		UserID:    request.UserID,
		SessionID: sessionID,
	})
	if err != nil {
		r.config.Logger.Warn("agent_session_state_unavailable",
			"run_id", request.RunID, "error", err)
		return domain.NewError(domain.CodeRuntimeSessionUnavailable, "read ADK session state", err)
	}
	if response == nil || response.Session == nil {
		return nil
	}
	value, err := response.Session.State().Get(structuredOutputStateKey)
	if err != nil {
		return nil
	}
	return state.consumeStructuredOutput(value, r.entrypoint)
}

// Entrypoint exposes the selected AgentDefinition to protocol adapters.
func (r *Runtime) Entrypoint() *AgentDefinition {
	return r.entrypoint
}

// Deployment exposes non-secret deployment metadata for standard protocol
// discovery. Instructions and schemas remain internal to the runtime.
func (r *Runtime) Deployment() *Deployment {
	return r.definition
}

func traceLogFields(ctx context.Context) []any {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return nil
	}
	return []any{"trace_id", spanContext.TraceID().String(), "span_id", spanContext.SpanID().String()}
}

func traceIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

func spanIDFromContext(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.SpanID().String()
}
