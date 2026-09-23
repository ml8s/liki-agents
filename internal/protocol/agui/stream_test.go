package agui

import (
	"testing"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestStreamEmitsEveryToolCallAndResultWithoutText(t *testing.T) {
	events := make([]aguievents.Event, 0)
	emit := func(event aguievents.Event) error {
		events = append(events, event)
		return nil
	}
	stream := &stream{runID: "run_1", messageID: "run_1:assistant", activeTools: make(map[string]string)}
	source := &session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
		{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "bazi_chart"}},
		{FunctionCall: &genai.FunctionCall{ID: "call_2", Name: "ziwei_chart"}},
	}}}}
	response := &session.Event{LLMResponse: model.LLMResponse{Content: &genai.Content{Parts: []*genai.Part{
		{FunctionResponse: &genai.FunctionResponse{ID: "call_1", Name: "bazi_chart", Response: map[string]any{"ok": true}}},
		{FunctionResponse: &genai.FunctionResponse{ID: "call_2", Name: "ziwei_chart", Response: map[string]any{"ok": true}}},
	}}}}

	if err := stream.consume(source, emit); err != nil {
		t.Fatalf("consume calls: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("call events = %d", len(events))
	}
	events = nil
	if err := stream.consume(response, emit); err != nil {
		t.Fatalf("consume responses: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("response events = %d", len(events))
	}
	result, ok := events[0].(*aguievents.ToolCallResultEvent)
	if !ok || result.MessageID != "run_1:assistant" || result.ToolCallID != "call_1" {
		t.Fatalf("first result = %#v", events[0])
	}
}

func TestFinishSynthesizesTextLifecycleForStructuredAnswer(t *testing.T) {
	events := make([]aguievents.Event, 0)
	stream := &stream{runID: "run_1", messageID: "run_1:assistant", activeTools: make(map[string]string)}
	if err := stream.finish("完整结论", func(event aguievents.Event) error {
		events = append(events, event)
		return nil
	}); err != nil {
		t.Fatalf("finish: %v", err)
	}
	want := []aguievents.EventType{
		aguievents.EventTypeTextMessageStart,
		aguievents.EventTypeTextMessageContent,
		aguievents.EventTypeTextMessageEnd,
	}
	if len(events) != len(want) {
		t.Fatalf("events = %d, want %d", len(events), len(want))
	}
	for index, kind := range want {
		if events[index].Type() != kind {
			t.Fatalf("events[%d] = %s, want %s", index, events[index].Type(), kind)
		}
	}
}
