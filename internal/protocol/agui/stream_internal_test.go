package agui

import (
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestStreamGeneratesUniqueFallbackToolIDs(t *testing.T) {
	streamer := &stream{runID: "run_1", entrypoint: "main"}
	event := func() *session.Event {
		return &session.Event{
			Author: "main",
			LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "same_tool"}},
				{FunctionResponse: &genai.FunctionResponse{Name: "same_tool", Response: map[string]any{}}},
			}}},
		}
	}
	emit := func(aguievents.Event) error { return nil }

	if err := streamer.consume(event(), emit); err != nil {
		t.Fatalf("first consume() error = %v", err)
	}
	first := streamer.createToolCallID("same_tool", "")
	if err := streamer.consume(event(), emit); err != nil {
		t.Fatalf("second consume() error = %v", err)
	}
	second := streamer.createToolCallID("same_tool", "")
	if first == "" || second == "" || first == second {
		t.Fatalf("fallback tool IDs = %q/%q, want unique values", first, second)
	}
}
