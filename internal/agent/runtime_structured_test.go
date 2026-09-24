package agent

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

var (
	testDeploymentOnce  sync.Once
	testDeploymentValue *Deployment
	testDeploymentErr   error
)

func buildTestDeployment() (*Deployment, error) {
	root, err := os.MkdirTemp("", "liki-agents-deployment-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(root)
	files := map[string]string{
		"agent-deployment.json": `{
			"apiVersion": "agent.liki/v1",
			"kind": "AgentDeployment",
			"metadata": {"name": "test-agent", "version": "1.0.0"},
			"spec": {
				"agents": [{
					"name": "main",
					"version": "1.0.0",
					"description": "generic test agent",
					"mode": "chat",
					"sub_agents": [],
					"instruction": {"path": "instruction.md"},
					"output": {
						"schema": {"path": "output.schema.json"},
						"textPointer": "/answer"
					},
					"tools": {"allow": ["test_tool"]}
									}]
			}
		}`,
		"instruction.md": "You are a generic test agent.",
		"output.schema.json": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"additionalProperties": false,
			"required": ["answer"],
			"properties": {
				"answer": {"type": "string", "minLength": 1},
				"confidence": {"type": "number", "minimum": 0, "maximum": 1}
			}
		}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			return nil, err
		}
	}
	return LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
}

func testDeployment() (*Deployment, error) {
	return buildTestDeployment()
}

func TestRunStateRejectsInvalidStructuredOutput(t *testing.T) {
	state := &runState{}
	deployment := NewTestDeployment(t)
	entrypoint, err := deployment.EntrypointDefinition()
	if err != nil {
		t.Fatal(err)
	}
	err = state.consumeStructuredOutput(map[string]any{"unexpected": true}, entrypoint)
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeStructuredOutputInvalid {
		t.Fatalf("consumeStructuredOutput() error = %v, want %s", err, domain.CodeStructuredOutputInvalid)
	}
}

func TestEventProjectorFiltersObserverFacts(t *testing.T) {
	toolCall := &genai.Part{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "test_tool"}}
	testCases := []struct {
		name    string
		event   *session.Event
		wantNil bool
	}{
		{name: "partial payload", event: &session.Event{
			LLMResponse: model.LLMResponse{Partial: true, Content: &genai.Content{Parts: []*genai.Part{{Text: `{"answer":"par`}}}},
		}, wantNil: true},
		{name: "partial tool call", event: &session.Event{
			LLMResponse: model.LLMResponse{Partial: true, Content: &genai.Content{Parts: []*genai.Part{toolCall}}},
		}, wantNil: true},
		{name: "final payload with tool fact", event: &session.Event{
			LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: `{"answer":"raw"}`}, toolCall}}},
		}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			projector := newEventProjector(NewTestDeployment(t))
			visible, visibleOK := projector.Project(testCase.event)
			if testCase.wantNil {
				if visibleOK {
					t.Fatalf("visible event = %+v, want none", visible)
				}
				return
			}
			if !visibleOK || visible == nil || len(visible.Content.Parts) != 1 || visible.Content.Parts[0].FunctionCall == nil {
				t.Fatalf("visible event = %+v/%v, want one function call", visible, visibleOK)
			}
		})
	}
}

type fakeLLM struct {
	responses []*model.LLMResponse
	err       error
}

func (f *fakeLLM) Name() string { return "fake-model" }

func (f *fakeLLM) GenerateContent(context.Context, *model.LLMRequest, bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, response := range f.responses {
			if !yield(response, nil) {
				return
			}
		}
		if f.err != nil {
			yield(nil, f.err)
		}
	}
}

type recordingAudit struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAudit) Record(_ context.Context, event *audit.Event) error {
	if event == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *event)
	return nil
}

func (r *recordingAudit) eventsOfType(eventType audit.EventType) []audit.Event {
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

// newTestRuntime wires a fake model and an in-memory MCP Engine. This avoids
// network dependencies while retaining ADK's real runner and toolset contract.
func newTestRuntime(t *testing.T, deployment *Deployment, events *recordingAudit, llm *fakeLLM) *Runtime {
	t.Helper()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(&mcp.Implementation{Name: "stub-engine", Version: "test"}, nil)
	server.AddTool(
		&mcp.Tool{Name: "test_tool", InputSchema: &jsonschema.Schema{Type: "object"}},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		},
	)
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connect test Engine: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	entrypoint, err := deployment.EntrypointDefinition()
	if err != nil {
		t.Fatalf("select entrypoint: %v", err)
	}
	structuredOutput := StructuredOutputNone
	if entrypoint.Output.Structured() {
		structuredOutput = StructuredOutputJSONSchema
	}
	runtime, err := NewRuntime(Config{
		Model:                   "fake-model",
		Deployment:              deployment,
		StructuredOutput:        structuredOutput,
		EngineMCPURL:            "in-memory://test-engine",
		AuditRecorder:           events,
		ContractVersion:         "test-contract",
		modelOverride:           llm,
		engineTransportOverride: clientTransport,
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

func plainTestDeployment(t *testing.T) *Deployment {
	t.Helper()
	deployment := NewTestDeployment(t)
	for index := range deployment.Spec.Agents {
		deployment.Spec.Agents[index].Output = OutputDefinition{}
		deployment.Spec.Agents[index].OutputSchema = nil
		deployment.Spec.Agents[index].ResolvedOutput = nil
		deployment.Spec.Agents[index].GenaiOutputSchema = nil
		deployment.Spec.Agents[index].SchemaDigest = ""
	}
	if err := deployment.validate(); err != nil {
		t.Fatalf("validate plain deployment: %v", err)
	}
	return deployment
}

func TestRuntimeRunReturnsPlainTextForGenericAgent(t *testing.T) {
	answer := "generic plain-text answer"
	events := &recordingAudit{}
	runtime := newTestRuntime(t, plainTestDeployment(t), events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: answer}}},
	}}})
	result, err := runtime.Run(context.Background(), RunRequest{
		RunID: "run_plain", ThreadID: "thread_plain", UserID: "user_1", UserMessage: "hello",
	}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Text != answer {
		t.Fatalf("Text = %q, want %q", result.Text, answer)
	}
	if len(result.Output) != 0 {
		t.Fatalf("Output = %s, want empty", result.Output)
	}
	if got := len(events.eventsOfType(audit.EventRunCompleted)); got != 1 {
		t.Fatalf("completed run audit events = %d, want 1", got)
	}
}

// TestRuntimeRunProducesStructuredAnswerWithoutPayloadLeak is the keystone
// regression test for the ADK structured-output contract: the Runner clears
// Event.Output before yielding, so the runtime must read the parsed answer
// from session state, must never expose payload JSON to protocol observers,
// and must still persist the LLM audit lifecycle.
func TestRuntimeRunProducesStructuredAnswerWithoutPayloadLeak(t *testing.T) {
	payload := `{"answer":"八字显示压力而非确定性。","confidence":0.82}`
	events := &recordingAudit{}
	runtime := newTestRuntime(t, NewTestDeployment(t), events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: payload}}},
	}}})

	observed := make([]*session.Event, 0)
	result, err := runtime.Run(context.Background(), RunRequest{
		RunID:       "run_1",
		ThreadID:    "thread_1",
		UserID:      "user_1",
		UserMessage: "看事业",
	}, func(event *session.Event) error {
		observed = append(observed, event)
		return nil
	})
	if err != nil {
		for cause := err; cause != nil; cause = errors.Unwrap(cause) {
			t.Logf("caused by: %v", cause)
		}
		t.Fatalf("Run() error = %v", err)
	}

	if result.Text != "八字显示压力而非确定性。" {
		t.Fatalf("Text = %q, want the parsed answer", result.Text)
	}
	if result.Definition.Name != "test-agent" || result.Definition.Version != "1.0.0" {
		t.Fatalf("definition ref = %+v", result.Definition)
	}
	var output map[string]any
	if err := json.Unmarshal(result.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	for index, event := range observed {
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part != nil && part.Text != "" && !part.Thought {
				t.Fatalf("observed[%d] leaks model payload text %q", index, part.Text)
			}
		}
	}
	for _, eventType := range []audit.EventType{
		audit.EventRunStarted,
		audit.EventLLMCallStarted,
		audit.EventLLMCallCompleted,
		audit.EventRunCompleted,
	} {
		if got := len(events.eventsOfType(eventType)); got != 1 {
			t.Fatalf("%s audit events = %d, want 1", eventType, got)
		}
	}
	if strings.Contains(result.Text, `\"answer\"`) {
		t.Fatal("answer still contains JSON payload markers")
	}
}

func TestRuntimeRunEmptyResponse(t *testing.T) {
	events := &recordingAudit{}
	runtime := newTestRuntime(t, NewTestDeployment(t), events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: ""}}},
	}}})
	_, runErr := runtime.Run(context.Background(), RunRequest{
		RunID: "run_empty", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) || domainErr.Code != domain.CodeRuntimeEmptyResponse {
		t.Fatalf("Run() error = %v, want %s", runErr, domain.CodeRuntimeEmptyResponse)
	}
	if got := len(events.eventsOfType(audit.EventRunFailed)); got != 1 {
		t.Fatalf("failed run audit events = %d, want 1", got)
	}
}

func TestRuntimeRunMarksObserverFailureAsFailedRun(t *testing.T) {
	observerErr := errors.New("protocol write failed")
	events := &recordingAudit{}
	runtime := newTestRuntime(t, NewTestDeployment(t), events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{
			{Text: `{"answer":"ok"}`},
			{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "test_tool"}},
		}},
	}}})
	_, runErr := runtime.Run(context.Background(), RunRequest{
		RunID: "run_observer", ThreadID: "thread_1", UserID: "user_1", UserMessage: "test",
	}, func(*session.Event) error {
		return observerErr
	})
	if !errors.Is(runErr, observerErr) {
		t.Fatalf("Run() error = %v, want observer error", runErr)
	}
	if got := len(events.eventsOfType(audit.EventRunFailed)); got != 1 {
		t.Fatalf("failed run audit events = %d, want 1", got)
	}
}

func TestRuntimeRunContextCancelled(t *testing.T) {
	events := &recordingAudit{}
	runtime := newTestRuntime(t, NewTestDeployment(t), events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "答案"}}},
	}}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, runErr := runtime.Run(ctx, RunRequest{
		RunID: "run_cancel", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) || domainErr.Code != domain.CodeRuntimeCancelled {
		t.Fatalf("Run() error = %v, want %s", runErr, domain.CodeRuntimeCancelled)
	}
}

func TestRuntimeRunLLMError(t *testing.T) {
	events := &recordingAudit{}
	llmErr := errors.New("model overloaded")
	runtime := newTestRuntime(t, NewTestDeployment(t), events, &fakeLLM{err: llmErr})
	_, runErr := runtime.Run(context.Background(), RunRequest{
		RunID: "run_err", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) {
		t.Fatalf("Run() error = %v, want domain error", runErr)
	}
	if domainErr.Code != domain.CodeRuntimeFailed {
		t.Fatalf("error code = %s, want %s", domainErr.Code, domain.CodeRuntimeFailed)
	}
	if got := len(events.eventsOfType(audit.EventLLMCallFailed)); got != 1 {
		t.Fatalf("failed LLM audit events = %d, want 1", got)
	}
}

// NewTestDeployment exposes the generic test deployment to external adapter
// tests compiled into the same test binary.
func NewTestDeployment(t testing.TB) *Deployment {
	deployment, err := testDeployment()
	if err != nil {
		t.Fatal(err)
	}
	return deployment
}

func TestRuntimeBuildsStandardADKAgentGraph(t *testing.T) {
	deployment := NewTestDeployment(t)
	// Convert the generic single-agent test deployment into a two-Agent tree.
	deployment.Spec.Agents[0].Name = "coordinator"
	deployment.Spec.Agents[0].SubAgents = []AgentReference{{Name: "worker"}}
	deployment.Spec.Agents = append(deployment.Spec.Agents, AgentDefinition{
		Name:        "worker",
		Version:     "1.0.0",
		Description: "generic worker agent",
		Mode:        AgentModeTask,
		Instruction: deployment.Spec.Agents[0].Instruction,
		Output:      deployment.Spec.Agents[0].Output,
		Tools:       ToolAllowlist{Allow: []string{"test_tool"}},
	})
	if err := deployment.validate(); err != nil {
		t.Fatalf("validate multi-agent deployment: %v", err)
	}
	for index := range deployment.Spec.Agents {
		agent := &deployment.Spec.Agents[index]
		if err := agent.validate(); err != nil {
			t.Fatalf("validate agent %q: %v", agent.Name, err)
		}
	}

	events := &recordingAudit{}
	runtime := newTestRuntime(t, deployment, events, &fakeLLM{responses: []*model.LLMResponse{{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"ok"}`}}},
	}}})
	if runtime.entrypoint.Name != "coordinator" {
		t.Fatalf("entrypoint = %q", runtime.entrypoint.Name)
	}
	worker := runtime.RootAgent().FindSubAgent("worker")
	if worker == nil {
		t.Fatal("worker sub-agent was not registered with ADK")
	}
	if worker.Name() != "worker" {
		t.Fatalf("worker name = %q", worker.Name())
	}
}
