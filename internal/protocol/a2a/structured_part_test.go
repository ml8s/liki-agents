package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/ml8s/liki-agents/internal/agent"
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
	part, err := agentPart(event, &genai.Part{Text: "plain answer"}, false, "main", "")
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
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"par`}, true, "main", "/answer")
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
			agent.StructuredOutputStateKey: map[string]any{"answer": " 最终结论 "},
		}},
	}
	event.Author = "main"
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"raw payload"}`}, true, "main", "/answer")
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	text, ok := part.Content.(a2a.Text)
	if !ok || text != "最终结论" {
		t.Fatalf("artifact text = %#v, want trimmed final answer", part.Content)
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
	part, err := agentPart(event, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "test_tool"}}, true, "main", "/answer")
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
			agent.StructuredOutputStateKey: map[string]any{"answer": "worker answer"},
		}},
	}
	part, err := agentPart(event, &genai.Part{Text: `{"answer":"worker payload"}`}, true, "main", "/answer")
	if err != nil {
		t.Fatalf("agentPart() error = %v", err)
	}
	if part != nil {
		t.Fatalf("worker structured part = %#v, want nil", part)
	}
}
