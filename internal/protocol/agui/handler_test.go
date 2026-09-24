package agui_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/platform/identity"
	"github.com/ml8s/liki-agents/internal/protocol/agui"
	"github.com/ml8s/liki-agents/internal/testagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type recordingAuditEvents struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recordingAuditEvents) Record(_ context.Context, event *audit.Event) error {
	return nil
}

func newRuntime(t *testing.T) *agent.Runtime {
	t.Helper()
	runtime, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		ModelAPIKey:      "test-key",
		Deployment:       testagent.Deployment(t),
		StructuredOutput: "json_schema",
		EngineMCPURL:     "http://127.0.0.1:1/mcp",
		GraphVersion:     "test-graph",
		ContractVersion:  "test-contract",
		AuditRecorder:    &recordingAuditEvents{},
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

func TestAGUIRequiresPost(t *testing.T) {
	handler, err := agui.New(newRuntime(t), agui.Config{RunTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/ag-ui", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAGUIRejectsMalformedInput(t *testing.T) {
	handler, err := agui.New(newRuntime(t), agui.Config{RunTimeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/ag-ui", strings.NewReader("{"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

type scriptedRuntime struct {
	events []*session.Event
	result agent.RunResult
	err    error
}

func (r *scriptedRuntime) Run(_ context.Context, _ agent.RunRequest, observe func(*session.Event) error) (agent.RunResult, error) {
	for _, event := range r.events {
		if err := observe(event); err != nil {
			return agent.RunResult{}, err
		}
	}
	return r.result, r.err
}

func authenticatedRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/ag-ui", strings.NewReader(body))
	return request.WithContext(identity.WithIdentity(request.Context(), identity.Identity{UserID: "user_1"}))
}

func TestAGUIRejectsUnsupportedCapabilitiesInsteadOfDroppingThem(t *testing.T) {
	handler, err := agui.New(&scriptedRuntime{}, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	cases := map[string]string{
		"client tools":   `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}],"tools":[{"name":"x","description":"x","parameters":{}}]}`,
		"client state":   `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}],"state":{"step":1}}`,
		"system history": `{"threadId":"t","runId":"r","messages":[{"id":"s","role":"system","content":"rules"},{"id":"m","role":"user","content":"hi"}]}`,
		"multimodal":     `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":[{"type":"image","url":"https://example.test/a.png"}]}]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, authenticatedRequest(t, body))
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestAGUIEmitsStructuredAnswerAsTextLifecycle(t *testing.T) {
	runtime := &scriptedRuntime{
		result: agent.RunResult{Output: []byte(`{"answer":"结构化答案"}`), Text: "结构化答案"},
	}
	handler, err := agui.New(runtime, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := authenticatedRequest(t, `{"threadId":"thread_1","runId":"run_1","messages":[{"id":"m1","role":"user","content":"hello"}]}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	want := []string{"RUN_STARTED", "TEXT_MESSAGE_START", "TEXT_MESSAGE_CONTENT", "TEXT_MESSAGE_END", "RUN_FINISHED"}
	last := -1
	for _, kind := range want {
		index := strings.Index(body, kind)
		if index < 0 {
			t.Fatalf("missing %s in %s", kind, body)
		}
		if index < last {
			t.Fatalf("%s appeared out of order in %s", kind, body)
		}
		last = index
	}
}

func TestAGUISuppressesRunErrorOnClientDisconnect(t *testing.T) {
	runtime := &scriptedRuntime{err: context.Canceled}
	handler, err := agui.New(runtime, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}]}`))
	if strings.Contains(response.Body.String(), "RUN_ERROR") {
		t.Fatalf("client disconnect must not emit RUN_ERROR: %s", response.Body.String())
	}
}

func TestAGUIEmitsRunErrorOnDeadline(t *testing.T) {
	runtime := &scriptedRuntime{err: context.DeadlineExceeded}
	handler, err := agui.New(runtime, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, authenticatedRequest(t, `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"hi"}]}`))
	body := response.Body.String()
	if !strings.Contains(body, "RUN_ERROR") || !strings.Contains(body, "exceeded its deadline") {
		t.Fatalf("deadline must emit RUN_ERROR: %s", body)
	}
}

func TestAGUIEmitsFullToolCallSequenceWithStructuredAnswer(t *testing.T) {
	events := []*session.Event{
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "engine_tool_a", Args: map[string]any{"input_a": "value-a"}}}}}}},
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: "call_2", Name: "engine_tool_b", Args: map[string]any{"input_b": "value-b"}}}}}}},
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "engine_tool_a", Response: map[string]any{"result_a": "value-a"}}}}}}},
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
			{FunctionResponse: &genai.FunctionResponse{ID: "call_2", Name: "engine_tool_b", Response: map[string]any{"result_b": "value-b"}}}}}}},
		{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{{Text: "最终分析"}}}}},
	}
	runtime := &scriptedRuntime{
		events: events,
		result: agent.RunResult{
			Output: []byte(`{"answer":"最终分析"}`),
			Text:   "最终分析",
		},
	}
	handler, err := agui.New(runtime, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := authenticatedRequest(t, `{"threadId":"t","runId":"r","messages":[{"id":"m","role":"user","content":"看事业"}]}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	want := []string{
		"RUN_STARTED",
		"TOOL_CALL_START", "TOOL_CALL_ARGS",
		"TOOL_CALL_RESULT", "TOOL_CALL_END",
		"RUN_FINISHED",
	}
	last := -1
	for _, kind := range want {
		index := strings.Index(body, kind)
		if index < 0 {
			t.Fatalf("missing %s in SSE body:\n%s", kind, body)
		}
		if index < last {
			t.Fatalf("%s appeared out of order in SSE body:\n%s", kind, body)
		}
		last = index
	}
	if !strings.Contains(body, "engine_tool_a") {
		t.Fatalf("tool name not found in SSE body")
	}
	if !strings.Contains(body, "value-a") {
		t.Fatalf("tool result not found in SSE body")
	}
	for _, callID := range []string{"call_1", "call_2"} {
		if !strings.Contains(body, callID) {
			t.Fatalf("SSE body missing %s", callID)
		}
	}
	for _, kind := range []string{
		"TOOL_CALL_START", "TOOL_CALL_ARGS", "TOOL_CALL_RESULT", "TOOL_CALL_END",
	} {
		if got := strings.Count(body, `"type":"`+kind+`"`); got != 2 {
			t.Fatalf("%s appeared %d times, want 2", kind, got)
		}
	}
}

func TestAGUIPassesHistoryToRuntime(t *testing.T) {
	var captured agent.RunRequest
	runtime := &capturingRuntime{
		capture: &captured,
		result:  agent.RunResult{Output: []byte(`{"answer":"好的"}`), Text: "好的"},
	}
	handler, err := agui.New(runtime, agui.Config{RunTimeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := `{"threadId":"t1","runId":"r1","messages":[
		{"id":"m1","role":"user","content":"看事业"},
		{"id":"m2","role":"assistant","content":"事业方面有压力。"},
		{"id":"m3","role":"user","content":"那感情呢？"}
	]}`
	request := authenticatedRequest(t, body)
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if captured.UserMessage != "那感情呢？" {
		t.Fatalf("UserMessage = %q, want latest user message", captured.UserMessage)
	}
	if len(captured.History) != 2 {
		t.Fatalf("History length = %d, want 2", len(captured.History))
	}
	if captured.History[0].Role != agent.RoleUser || captured.History[0].Content != "看事业" {
		t.Fatalf("History[0] = %+v", captured.History[0])
	}
	if captured.History[1].Role != agent.RoleAssistant || captured.History[1].Content != "事业方面有压力。" {
		t.Fatalf("History[1] = %+v", captured.History[1])
	}
	if captured.ThreadID != "t1" || captured.RunID != "r1" {
		t.Fatalf("IDs = %q/%q", captured.ThreadID, captured.RunID)
	}
}

type capturingRuntime struct {
	capture *agent.RunRequest
	result  agent.RunResult
}

func (r *capturingRuntime) Run(_ context.Context, request agent.RunRequest, _ func(*session.Event) error) (agent.RunResult, error) {
	*r.capture = request
	return r.result, nil
}
