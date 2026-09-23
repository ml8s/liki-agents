package a2a

import (
	"testing"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/liki/liki-agent/internal/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestStructuredAwarePartDropsPartialPayloadText(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"par`}}},
		},
	}
	part, err := structuredAwarePart(event, &genai.Part{Text: `{"answer":"par`})
	if err != nil {
		t.Fatalf("structuredAwarePart() error = %v", err)
	}
	if part != nil {
		t.Fatalf("partial payload part = %+v, want nil", part)
	}
}

func TestStructuredAwarePartSubstitutesFinalAnswer(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: `{"answer":"raw payload"}`}}},
		},
		Actions: session.EventActions{StateDelta: map[string]any{
			agent.StructuredOutputStateKey: map[string]any{"answer": " 最终结论 "},
		}},
	}
	part, err := structuredAwarePart(event, &genai.Part{Text: `{"answer":"raw payload"}`})
	if err != nil {
		t.Fatalf("structuredAwarePart() error = %v", err)
	}
	text, ok := part.Content.(a2a.Text)
	if !ok || text != "最终结论" {
		t.Fatalf("artifact text = %#v, want trimmed final answer", part.Content)
	}
}

func TestStructuredAwarePartKeepsToolCallFacts(t *testing.T) {
	event := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "bazi_chart"}},
			}},
		},
	}
	part, err := structuredAwarePart(event, &genai.Part{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "bazi_chart"}})
	if err != nil {
		t.Fatalf("structuredAwarePart() error = %v", err)
	}
	if part == nil {
		t.Fatal("tool call part was dropped, want framework default mapping")
	}
}
