// Package agent owns the single ADK execution graph and its external model and
// Engine MCP dependencies. Protocol packages adapt this runtime; they never
// create a second execution path.
package agent

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/openaimodel"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/plugin/loggingplugin"
	"google.golang.org/adk/v2/plugin/retryandreflect"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/genai"
)

const (
	// structuredOutputStateKey matches llmagent.Config.OutputKey. ADK parses
	// the model reply against OutputSchema, clears Event.Output before
	// yielding it, and persists the parsed value into session state under
	// this key. Session state is the framework contract for consuming it.
	structuredOutputStateKey = "structured_analysis"
)

type Runtime struct {
	config   Config
	root     agent.Agent
	sessions session.Service
	runner   *runner.Runner
	plugins  runner.PluginConfig
	toolset  tool.Toolset
	llm      *llmLedger
}

func NewRuntime(config Config) (*Runtime, error) {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.AppName == "" {
		config.AppName = "liki-agent"
	}
	if config.AgentName == "" {
		config.AgentName = "chief_analyst"
	}
	if config.AgentDescription == "" {
		config.AgentDescription = "Grounded destiny analysis assistant"
	}
	if config.System == "" {
		config.System = "chief"
	}
	if config.ExpertName == "" {
		config.ExpertName = "chief_analyst"
	}
	if config.Model == "" {
		return nil, domain.NewError(domain.CodeLLMModelMissing, "LLM model is required", domain.ErrInvalidInput)
	}
	if len(config.AllowedTools) == 0 {
		return nil, domain.NewError(domain.CodeEngineToolsEmpty, "at least one Engine tool must be allowlisted", domain.ErrInvalidInput)
	}
	if config.ModelTimeout <= 0 {
		config.ModelTimeout = 120 * time.Second
	}
	if config.EngineMCPURL == "" {
		return nil, domain.NewError(domain.CodeEngineMCPURLMissing, "Engine MCP URL is required", domain.ErrInvalidInput)
	}
	if config.EngineTimeout <= 0 {
		config.EngineTimeout = 30 * time.Second
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.PromptVersion == "" {
		config.PromptVersion = "chief-analysis-v1"
	}
	if config.PolicyVersion == "" {
		config.PolicyVersion = "destiny-safety-v1"
	}
	// Protocol callers own durable conversation/product history. Each ADK
	// session is deliberately run-scoped working state and must not become a
	// second durable conversation store.
	sessionService := session.InMemoryService()

	var aiModel model.LLM
	if config.modelOverride != nil {
		aiModel = config.modelOverride
	} else {
		var err error
		// Use ADK's built-in openaimodel (Responses API). Zhipu supports the
		// Responses API at https://open.bigmodel.cn/api/v1. For providers that
		// only support Chat Completions, switch to ChatCompletionsModel.
		aiModel, err = openaimodel.NewModel(context.Background(), config.Model, &openaimodel.ClientConfig{
			APIKey:     config.ModelAPIKey,
			BaseURL:    config.ModelBaseURL,
			HTTPClient: &http.Client{Timeout: config.ModelTimeout},
		})
		if err != nil {
			return nil, domain.NewError(domain.CodeLLMUnavailable, "create LLM model", err)
		}
		if providerUsesJSONObjectOutput(config.Provider, config.ModelBaseURL) {
			compatModel, err := newJSONObjectModel(aiModel, structuredOutputSchema())
			if err != nil {
				return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create provider structured output model", err)
			}
			aiModel = compatModel
		}
	}
	temperature := float32(config.Temperature)
	ledger := newLLMLedger(config.LLMRecorder, config.Metrics, config.Provider, config.Now)
	toolset, err := newEngineToolset(config)
	if err != nil {
		return nil, err
	}
	plugins := make([]*plugin.Plugin, 0, 2)
	retryPlugin, err := retryandreflect.New(
		retryandreflect.WithMaxRetries(2),
		retryandreflect.WithErrorIfRetryExceeded(true),
	)
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create tool retry plugin", err)
	}
	plugins = append(plugins, retryPlugin)
	if config.Env == "development" {
		loggingPlugin, err := loggingplugin.New("liki_debug")
		if err != nil {
			return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create logging plugin", err)
		}
		plugins = append(plugins, loggingPlugin)
	}
	root, err := llmagent.New(llmagent.Config{
		Name:        config.AgentName,
		Description: config.AgentDescription,
		Model:       aiModel,
		Instruction: instruction(config),
		Toolsets:    []tool.Toolset{toolset},
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{
			ledger.beforeModel,
		},
		AfterModelCallbacks: []llmagent.AfterModelCallback{
			ledger.afterModel,
		},
		OnModelErrorCallbacks: []llmagent.OnModelErrorCallback{
			ledger.onModelError,
		},
		GenerateContentConfig: &genai.GenerateContentConfig{
			Temperature: &temperature,
		},
		OutputSchema: structuredOutputSchema(),
		OutputKey:    structuredOutputStateKey,
	})
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create ADK agent", err)
	}
	runnerInstance, err := runner.New(runner.Config{
		AppName:           config.AppName,
		Agent:             root,
		SessionService:    sessionService,
		AutoCreateSession: true,
		PluginConfig: runner.PluginConfig{
			Plugins:      plugins,
			CloseTimeout: 5 * time.Second,
		},
	})
	if err != nil {
		return nil, domain.NewError(domain.CodeRuntimeInitFailed, "create ADK runner", err)
	}
	return &Runtime{
		config:   config,
		root:     root,
		sessions: sessionService,
		runner:   runnerInstance,
		plugins:  runner.PluginConfig{Plugins: plugins, CloseTimeout: 5 * time.Second},
		toolset:  toolset,
		llm:      ledger,
	}, nil
}

func newEngineToolset(config Config) (tool.Toolset, error) {
	var credential auth.CredentialProvider
	if config.EngineToken != "" {
		credential = auth.StaticToken(config.EngineToken)
	}
	engineTools, err := mcptoolset.New(mcptoolset.Config{
		Transport: newEngineTransport(config),
		Auth:      credential,
	})
	if err != nil {
		return nil, domain.NewError(domain.CodeEngineToolsUnavailable, "create Engine MCP toolset", err)
	}
	return tool.FilterToolset(engineTools, tool.AllowedToolsPredicate(config.AllowedTools)), nil
}

// RootAgent exposes the single ADK graph to standard protocol adapters. ADK
// types do not cross further inward than this runtime component.
func (r *Runtime) RootAgent() agent.Agent {
	return r.root
}

// RunnerConfig returns the shared ADK runtime configuration used by protocol
// bindings. The same plugins and run-scoped session service are preserved.
func (r *Runtime) RunnerConfig() runner.Config {
	return runner.Config{
		AppName:           r.config.AppName,
		Agent:             r.root,
		SessionService:    r.sessions,
		AutoCreateSession: true,
		PluginConfig:      r.plugins,
	}
}

// AuditRunScope identifies an externally driven ADK execution for the LLM
// audit ledger. It does not create a second execution path.
type AuditRunScope struct {
	RunID    string
	ThreadID string
	UserID   string
	Product  string
}

// BeginAuditRun attaches protocol identity to model callbacks when an official
// protocol executor drives the shared ADK runtime directly.
func (r *Runtime) BeginAuditRun(sessionID string, scope AuditRunScope) error {
	scope.RunID = strings.TrimSpace(scope.RunID)
	scope.ThreadID = strings.TrimSpace(scope.ThreadID)
	scope.UserID = strings.TrimSpace(scope.UserID)
	scope.Product = strings.TrimSpace(scope.Product)
	if scope.RunID == "" || scope.ThreadID == "" || scope.UserID == "" {
		return domain.NewError(domain.CodeAuditScopeRequired, "run, thread, and user identifiers are required", domain.ErrInvalidInput)
	}
	if sessionID == "" {
		return domain.NewError(domain.CodeAuditSessionRequired, "audit session identifier is required", domain.ErrInvalidInput)
	}
	started := r.llm.begin(sessionID, &llmRunScope{
		runID:       domain.ID(scope.RunID),
		threadID:    domain.ID(scope.ThreadID),
		userID:      scope.UserID,
		agentName:   r.config.AgentName,
		model:       r.config.Model,
		product:     scope.Product,
		graph:       r.config.GraphVersion,
		contract:    r.config.ContractVersion,
		prompt:      r.config.PromptVersion,
		policy:      r.config.PolicyVersion,
		lastByModel: make(map[string]llmCallRuntime),
	})
	if !started {
		return domain.NewError(domain.CodeAuditSessionActive, "an audit run is already active for this session", domain.ErrInvalidInput)
	}
	return nil
}

// EndAuditRun completes an externally driven audit lifecycle exactly once.
func (r *Runtime) EndAuditRun(sessionID string, runErr error) {
	if sessionID == "" {
		return
	}
	r.llm.end(sessionID, runtimeError(runErr))
}

// Stream executes the shared ADK runtime and exposes native ADK events to a
// protocol adapter. It is not a public API and creates no second business path.
func (r *Runtime) Run(
	ctx context.Context,
	request RunRequest,
	observe func(*session.Event) error,
) (RunResult, error) {
	var runErr error
	state := &runState{
		request:         request,
		now:             r.config.Now,
		structured:      true,
		activeToolCalls: make(map[string]toolCallRuntime),
		lastToolCalls:   make(map[string]toolCallRuntime),
	}
	// Session identity is run-scoped. Thread identity remains owned by the
	// application database, preventing implicit duplication of durable history.
	sessionID := "run:" + request.RunID
	scope := &llmRunScope{
		runID:       domain.ID(request.RunID),
		threadID:    domain.ID(request.ThreadID),
		userID:      request.UserID,
		agentName:   r.config.AgentName,
		model:       r.config.Model,
		product:     request.Product,
		graph:       r.config.GraphVersion,
		contract:    r.config.ContractVersion,
		prompt:      r.config.PromptVersion,
		policy:      r.config.PolicyVersion,
		lastByModel: make(map[string]llmCallRuntime),
	}
	r.llm.begin(sessionID, scope)
	defer func() {
		r.llm.end(sessionID, runErr)
	}()
	r.config.Logger.Info("agent_run_started",
		"run_id", request.RunID,
		"thread_id", request.ThreadID,
		"user_id", request.UserID,
		"model", r.config.Model,
	)
	events := r.runner.Run(
		ctx,
		request.UserID,
		sessionID,
		buildUserContent(request),
		agent.RunConfig{StreamingMode: agent.StreamingModeSSE},
	)
	for event, err := range events {
		if err != nil {
			r.config.Logger.Warn("agent_run_failed",
				"run_id", request.RunID, "error", err)
			runErr = runtimeError(err)
			return RunResult{}, runErr
		}
		if observe != nil {
			if visible := visibleEvent(event); visible != nil {
				if err := observe(visible); err != nil {
					return RunResult{}, err
				}
			}
		}
		if err := state.consume(event); err != nil {
			return RunResult{}, err
		}
	}
	if state.analysis == nil {
		if err := r.loadStructuredAnalysis(ctx, request, sessionID, state); err != nil {
			return RunResult{}, err
		}
	}

	var content string
	confidence := unstructuredFallbackConfidence
	var topic string
	var supportingFactors []string
	var limitations []string
	structuredUsed := state.analysis != nil
	if structuredUsed {
		content = strings.TrimSpace(state.analysis.Answer)
		confidence = state.analysis.Confidence
		topic = state.analysis.Topic
		supportingFactors = state.analysis.KeyFactors
		limitations = state.analysis.Limitations
	} else {
		content = strings.TrimSpace(state.final.String())
		limitations = []string{"structured analysis output was unavailable; response was downgraded to text"}
	}
	if content == "" {
		r.config.Logger.Warn("agent_run_empty_response", "run_id", request.RunID)
		runErr = domain.NewError(domain.CodeRuntimeEmptyResponse, "ADK runtime returned no final response", nil)
		return RunResult{}, runErr
	}
	opinion := domain.ExpertOpinion{
		Expert:            r.config.ExpertName,
		System:            r.config.System,
		Topic:             topic,
		Conclusion:        content,
		Confidence:        confidence,
		SupportingFactors: supportingFactors,
		Limitations:       limitations,
		SourceTools:       state.tools(),
		CreatedAt:         r.config.Now(),
		Metadata: map[string]any{
			"model":          r.config.Model,
			"policy_version": r.config.PolicyVersion,
			"prompt_version": r.config.PromptVersion,
			"runtime":        "google-adk-go/v2",
			"structured":     structuredUsed,
		},
	}
	if err := opinion.Validate(); err != nil {
		r.config.Logger.Warn("agent_run_opinion_invalid",
			"run_id", request.RunID, "error", err)
		runErr = err
		return RunResult{}, runErr
	}
	r.config.Logger.Info("agent_run_completed",
		"run_id", request.RunID,
		"topic", opinion.Topic,
		"confidence", opinion.Confidence,
		"structured", structuredUsed,
		"tools_used", state.tools(),
		"model", r.config.Model,
	)
	return RunResult{
		FinalContent:   content,
		ExpertOpinions: []domain.ExpertOpinion{opinion},
		SourceTools:    state.tools(),
		Model:          r.config.Model,
	}, nil
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
	return state.consumeStructuredOutput(value)
}

// visibleEvent strips model-generated structured payload text before native
// events cross the runtime boundary. In the structured-output graph, model
// text is protocol payload (JSON), never user-facing message content. Function
// call and response facts remain visible to protocol adapters.
