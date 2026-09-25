package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/testagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestAgentPartPassesPlainTextThroughFrameworkMapping(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "plain answer"}}},
		},
	}
	part, err := agentPart(event, &genai.Part{Text: "plain answer"}, false, nil)
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	text, ok := part.Content.(a2a.Text)
	if !ok || text != "plain answer" {
		t.Fatalf("artifact text = %#v, want framework-mapped plain answer", part.Content)
	}
}

func TestAgentPartDropsPartialPayloadText(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"par`}}},
		},
	}
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"par`}, true, structuredEntrypoint(t))
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	if part != nil {
		t.Fatalf("partial payload part = %+v, want nil", part)
	}
}

func TestAgentPartSubstitutesDeclaredPointer(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"raw payload"}`}}},
		},
		Actions: session.EventActions{StateDelta: map[string]any{
			agent.StructuredOutputStateKey("main"): map[string]any{"answer": " 最终结论 "},
		}},
	}
	event.Author = "main"
	entrypoint := structuredEntrypoint(t)
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"raw payload"}`}, true, entrypoint)
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	data, ok := part.Content.(a2a.Data)
	if !ok {
		t.Fatalf("artifact content = %#v, want structured data", part.Content)
	}
	output, outputOK := data.Value.(map[string]any)
	if !outputOK || output["answer"] != " 最终结论 " {
		t.Fatalf("artifact data = %#v, want validated JSON output", data.Value)
	}
	if part.MediaType != "application/json" {
		t.Fatalf("artifact media type = %q", part.MediaType)
	}
	if part.Metadata["liki.answer"] != "最终结论" {
		t.Fatalf("artifact answer metadata = %#v", part.Metadata)
	}
}

func TestAgentPartKeepsToolCallFacts(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "test_tool"}},
			}},
		},
	}
	part, err := agentPart(event, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "test_tool"}}, true, structuredEntrypoint(t))
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	if part == nil {
		t.Fatal("tool call part was dropped, want framework default mapping")
	}
}

func TestAgentPartIgnoresNonEntrypointStructuredOutput(t *testing.T) {
	event := &session.Event{
		Author: "worker",
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"worker payload"}`}}},
		},
		Actions: session.EventActions{StateDelta: map[string]any{
			agent.StructuredOutputStateKey("worker"): map[string]any{"answer": "worker answer"},
		}},
	}
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"worker payload"}`}, true, structuredEntrypoint(t))
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	if part != nil {
		t.Fatalf("worker structured part = %#v, want nil", part)
	}
}

func structuredEntrypoint(t *testing.T) *agent.AgentDefinition {
	t.Helper()
	entrypoint, err := testagent.Deployment(t).EntrypointDefinition()
	if err != nil {
		t.Fatal(err)
	}
	return entrypoint
}
