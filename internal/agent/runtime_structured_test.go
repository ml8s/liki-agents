package agent

import (
	"context"
	"errors"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestStructuredOutputSchemaContract(t *testing.T) {
	schema := structuredOutputSchema()
	expected := []string{"answer", "confidence", "topic", "key_factors", "limitations"}
	if len(schema.Required) != len(expected) {
		t.Fatalf("required fields = %v, want %v", schema.Required, expected)
	}
	for _, field := range expected {
		if schema.Properties[field] == nil {
			t.Fatalf("schema is missing %q", field)
		}
	}
	if got := schema.Properties["confidence"].Type; got != genai.TypeNumber {
		t.Fatalf("confidence type = %q, want number", got)
	}
	if got := schema.Properties["key_factors"].Items.Type; got != genai.TypeString {
		t.Fatalf("key_factors item type = %q, want string", got)
	}
	if got := schema.Properties["answer"].MinLength; got == nil || *got != 1 {
		t.Fatalf("answer min length = %v, want 1", got)
	}
}

type fakeSessionState struct {
	values map[string]any
}

func (s *fakeSessionState) Get(key string) (any, error) {
	value, ok := s.values[key]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return value, nil
}

func (s *fakeSessionState) Set(key string, value any) error {
	s.values[key] = value
	return nil
}

func (s *fakeSessionState) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for key, value := range s.values {
			if !yield(key, value) {
				return
			}
		}
	}
}

type fakeSession struct {
	state session.State
}

func (s *fakeSession) ID() string                { return "session_1" }
func (s *fakeSession) AppName() string           { return "liki-agent" }
func (s *fakeSession) UserID() string            { return "user_1" }
func (s *fakeSession) State() session.State      { return s.state }
func (s *fakeSession) Events() session.Events    { return nil }
func (s *fakeSession) LastUpdateTime() time.Time { return time.Time{} }

type fakeSessionService struct {
	session.Service
	state session.State
}

func (s *fakeSessionService) Get(context.Context, *session.GetRequest) (*session.GetResponse, error) {
	return &session.GetResponse{Session: &fakeSession{state: s.state}}, nil
}

func TestRuntimeLoadsStructuredOutputFromSessionState(t *testing.T) {
	runtime := &Runtime{
		config: Config{AppName: "liki-agent"},
		sessions: &fakeSessionService{state: &fakeSessionState{values: map[string]any{
			structuredOutputStateKey: map[string]any{
				"answer":      "The chart shows pressure, not certainty.",
				"confidence":  0.82,
				"topic":       "career",
				"key_factors": []any{"seven killings is heavy"},
				"limitations": []any{"not financial advice"},
			},
		}}},
	}
	state := &runState{request: RunRequest{RunID: "run_1", ThreadID: "thread_1", UserID: "user_1"}}
	if err := runtime.loadStructuredAnalysis(context.Background(), state.request, "session_1", state); err != nil {
		t.Fatalf("loadStructuredAnalysis() error = %v", err)
	}
	if state.analysis == nil {
		t.Fatal("analysis was not loaded from session state")
	}
	if state.analysis.Answer != "The chart shows pressure, not certainty." || state.analysis.Topic != "career" || state.analysis.Confidence != 0.82 {
		t.Fatalf("analysis = %+v", state.analysis)
	}
}

func TestRunStateSuppressesStructuredPartialJSON(t *testing.T) {
	state := &runState{structured: true}
	first := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"par`}}},
		},
	}
	second := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `tial"}"`}}},
		},
	}
	if err := state.consume(first); err != nil {
		t.Fatalf("first consume() error = %v", err)
	}
	if err := state.consume(second); err != nil {
		t.Fatalf("second consume() error = %v", err)
	}
	if state.final.Len() != 0 {
		t.Fatalf("raw structured partial was retained: %q", state.final.String())
	}
}

func TestRunStateRejectsInvalidStructuredOutput(t *testing.T) {
	state := &runState{}
	err := state.consumeStructuredOutput(map[string]any{"confidence": 0.9})
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != "structured_output_empty" {
		t.Fatalf("consumeStructuredOutput() error = %v, want structured_output_empty", err)
	}
}

func TestVisibleEventStripsStructuredPayloadText(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"par`}}},
		},
	}
	visible := visibleEvent(event)
	if visible != nil {
		t.Fatalf("visible event = %+v, want nil for pure payload text", visible)
	}
}

func TestVisibleEventKeepsToolFactsAndDropsPayloadText(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{Text: `{"answer":"raw"}`},
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "bazi_chart"}},
			}},
		},
	}
	visible := visibleEvent(event)
	if visible == nil {
		t.Fatal("visible event was nil, want tool facts")
	}
	if len(visible.Content.Parts) != 1 || visible.Content.Parts[0].FunctionCall == nil {
		t.Fatalf("visible parts = %+v, want only the function call", visible.Content.Parts)
	}
}

// newStubEngineMCP serves a real in-process Engine MCP so the ADK toolset can
// list tools exactly as it does in production. The deterministic tool is never
// invoked because the fake model never emits a function call.
func newStubEngineMCP() http.Handler {
	server := mcp.NewServer(&mcp.Implementation{Name: "stub-engine", Version: "v1"}, nil)
	server.AddTool(&mcp.Tool{Name: "bazi_chart", Description: "stub deterministic chart", InputSchema: &jsonschema.Schema{Type: "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
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
	starts   int
	finishes int
	last     *domain.LLMCall
}

func (r *recordingAudit) Start(_ context.Context, call *domain.LLMCall) error {
	r.starts++
	r.last = call
	return nil
}

func (r *recordingAudit) Finish(_ context.Context, call *domain.LLMCall) error {
	r.finishes++
	r.last = call
	return nil
}

// TestRuntimeRunProducesStructuredAnswerWithoutPayloadLeak is the keystone
// regression test for the ADK structured-output contract: the Runner clears
// Event.Output before yielding, so the runtime must read the parsed answer
// from session state, must never expose payload JSON to protocol observers,
// and must still persist the LLM audit lifecycle.
func TestRuntimeRunProducesStructuredAnswerWithoutPayloadLeak(t *testing.T) {
	payload := `{"answer":"八字显示压力而非确定性。","confidence":0.82,"topic":"career","key_factors":["七杀重"],"limitations":["非投资建议"]}`
	audit := &recordingAudit{}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("network listen unavailable in this environment: %v", err)
	}
	engine := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: newStubEngineMCP()},
	}
	engine.Start()
	t.Cleanup(func() {
		engine.CloseClientConnections()
		engine.Close()
	})
	runtime, err := NewRuntime(Config{
		Model:           "fake-model",
		AllowedTools:    []string{"bazi_chart"},
		EngineMCPURL:    engine.URL,
		LLMRecorder:     audit,
		ContractVersion: "test-contract",
		modelOverride: &fakeLLM{responses: []*model.LLMResponse{{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: payload}}},
		}}},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}

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

	if result.FinalContent != "八字显示压力而非确定性。" {
		t.Fatalf("FinalContent = %q, want the parsed answer", result.FinalContent)
	}
	if len(result.ExpertOpinions) != 1 {
		t.Fatalf("expert opinions = %d, want 1", len(result.ExpertOpinions))
	}
	opinion := result.ExpertOpinions[0]
	if opinion.Confidence != 0.82 || opinion.Topic != "career" {
		t.Fatalf("opinion confidence/topic = %v/%q", opinion.Confidence, opinion.Topic)
	}
	if structured, ok := opinion.Metadata["structured"].(bool); !ok || !structured {
		t.Fatalf("opinion structured metadata = %v", opinion.Metadata["structured"])
	}
	for index, event := range observed {
		if event.Content != nil && textFromContent(event.Content) != "" {
			t.Fatalf("observed[%d] leaks model payload text %q", index, textFromContent(event.Content))
		}
	}
	if audit.starts != 1 || audit.finishes != 1 {
		t.Fatalf("audit lifecycle = %d starts / %d finishes, want 1/1", audit.starts, audit.finishes)
	}
	if audit.last == nil || audit.last.Status != domain.LLMCallCompleted {
		t.Fatalf("audit last call = %+v, want completed", audit.last)
	}
	if audit.last != nil && audit.last.RunID != "run_1" {
		t.Fatalf("audit run id = %q", audit.last.RunID)
	}
	if strings.Contains(result.FinalContent, "answer") {
		t.Fatal("FinalContent still contains JSON payload markers")
	}
}

func TestRuntimeRunEmptyResponse(t *testing.T) {
	audit := &recordingAudit{}
	engine := newTestEngine(t)
	runtime, err := NewRuntime(Config{
		Model:           "fake-model",
		AllowedTools:    []string{"bazi_chart"},
		EngineMCPURL:    engine.URL,
		LLMRecorder:     audit,
		ContractVersion: "test-contract",
		modelOverride: &fakeLLM{responses: []*model.LLMResponse{{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: ""}}},
		}}},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	_, runErr := runtime.Run(context.Background(), RunRequest{
		RunID: "run_empty", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) || domainErr.Code != "runtime_empty_response" {
		t.Fatalf("Run() error = %v, want runtime_empty_response", runErr)
	}
	if audit.last != nil && audit.last.Status != domain.LLMCallCompleted {
		t.Fatalf("audit status = %s, want completed", audit.last.Status)
	}
}

func TestRuntimeRunContextCancelled(t *testing.T) {
	audit := &recordingAudit{}
	engine := newTestEngine(t)
	runtime, err := NewRuntime(Config{
		Model:           "fake-model",
		AllowedTools:    []string{"bazi_chart"},
		EngineMCPURL:    engine.URL,
		LLMRecorder:     audit,
		ContractVersion: "test-contract",
		modelOverride: &fakeLLM{responses: []*model.LLMResponse{{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "答案"}}},
		}}},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, runErr := runtime.Run(ctx, RunRequest{
		RunID: "run_cancel", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) || domainErr.Code != "runtime_cancelled" {
		t.Fatalf("Run() error = %v, want runtime_cancelled", runErr)
	}
}

func TestRuntimeRunLLMError(t *testing.T) {
	audit := &recordingAudit{}
	engine := newTestEngine(t)
	llmErr := errors.New("model overloaded")
	runtime, err := NewRuntime(Config{
		Model:           "fake-model",
		AllowedTools:    []string{"bazi_chart"},
		EngineMCPURL:    engine.URL,
		LLMRecorder:     audit,
		ContractVersion: "test-contract",
		modelOverride: &fakeLLM{
			err: llmErr,
		},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	_, runErr := runtime.Run(context.Background(), RunRequest{
		RunID: "run_err", ThreadID: "thread_1", UserID: "user_1", UserMessage: "看事业",
	}, nil)
	var domainErr *domain.Error
	if !errors.As(runErr, &domainErr) {
		t.Fatalf("Run() error = %v, want domain error", runErr)
	}
	if domainErr.Code != "runtime_failed" {
		t.Fatalf("error code = %s, want runtime_failed", domainErr.Code)
	}
	if audit.last == nil || audit.last.Status != domain.LLMCallFailed {
		t.Fatalf("audit should record failed LLM call, got %+v", audit.last)
	}
}

func newTestEngine(t *testing.T) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("network listen unavailable: %v", err)
	}
	engine := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: newStubEngineMCP()},
	}
	engine.Start()
	t.Cleanup(func() {
		engine.CloseClientConnections()
		engine.Close()
	})
	return engine
}
