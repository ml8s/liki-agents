package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ml8s/liki-agents/internal/domain"
	jsonpointer "github.com/qri-io/jsonpointer"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type runState struct {
	output         OutputResult
	capturePlain   bool
	plainAgentName string
	tools          toolNameTracker
}

type OutputResult struct {
	JSON json.RawMessage
	Text string
}

type toolNameTracker struct {
	seen []string
}

func (t *toolNameTracker) consume(event *session.Event) {
	if event == nil || event.Content == nil {
		return
	}
	for _, part := range event.Content.Parts {
		if part == nil || part.FunctionResponse == nil {
			continue
		}
		name := part.FunctionResponse.Name
		seen := false
		for _, value := range t.seen {
			if value == name {
				seen = true
				break
			}
		}
		if !seen {
			t.seen = append(t.seen, name)
		}
	}
}

func (t *toolNameTracker) Names() []string {
	result := make([]string, len(t.seen))
	copy(result, t.seen)
	return result
}

func (s *runState) consume(event *session.Event) {
	s.tools.consume(event)
	if !s.capturePlain || event == nil || event.Partial ||
		event.Author != s.plainAgentName || !event.IsFinalResponse() {
		return
	}
	if event.Content == nil {
		return
	}
	var text strings.Builder
	for _, part := range event.Content.Parts {
		if part == nil || part.Text == "" || part.Thought {
			continue
		}
		text.WriteString(part.Text)
	}
	if value := strings.TrimSpace(text.String()); value != "" {
		s.output = OutputResult{Text: value}
	}
}

func (s *runState) consumeStructuredOutput(value any, definition *AgentDefinition) error {
	var document any
	switch typed := value.(type) {
	case string:
		if err := json.Unmarshal([]byte(typed), &document); err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "decode structured agent output", err)
		}
	default:
		raw, err := json.Marshal(typed)
		if err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "encode structured agent output", err)
		}
		if err := json.Unmarshal(raw, &document); err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "decode structured agent output", err)
		}
	}
	if err := definition.ResolvedOutput.Validate(document); err != nil {
		return domain.NewError(domain.CodeStructuredOutputInvalid, "validate structured agent output", err)
	}
	text, err := jsonStringAtPointer(document, definition.Output.TextPointer)
	if err != nil {
		return domain.NewError(domain.CodeStructuredOutputInvalid, "extract structured answer text", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return domain.NewError(domain.CodeStructuredOutputEmpty, "structured agent output has no answer text", nil)
	}
	s.output = OutputResult{JSON: canonicalJSON(document), Text: text}
	return nil
}

// OutputText extracts the Agent-declared user-facing text from validated
// structured output. Protocol adapters use this so they cannot hard-code a
// domain schema shape.
func OutputText(value any, pointer string) (string, error) {
	return jsonStringAtPointer(value, pointer)
}

func jsonStringAtPointer(document any, pointer string) (string, error) {
	pointerValue, err := jsonpointer.Parse(pointer)
	if err != nil {
		return "", fmt.Errorf("parse JSON Pointer: %w", err)
	}
	if pointerValue.IsEmpty() {
		if text, ok := document.(string); ok {
			return text, nil
		}
		return "", fmt.Errorf("root value is not a string")
	}
	value, err := pointerValue.Eval(document)
	if err != nil {
		return "", fmt.Errorf("evaluate JSON Pointer: %w", err)
	}
	current := value
	text, ok := current.(string)
	if !ok {
		return "", fmt.Errorf("pointer %q does not select a string", pointer)
	}
	return text, nil
}

func canonicalJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return []byte("null")
	}
	return raw
}

func buildUserContent(request RunRequest) *genai.Content {
	var prompt strings.Builder
	prompt.WriteString("<request_context>")
	if len(request.Context) != 0 && json.Valid(request.Context) {
		prompt.Write(request.Context)
	} else {
		prompt.WriteString("{}")
	}
	prompt.WriteString("</request_context>\n<user_message>")
	prompt.WriteString(request.UserMessage)
	prompt.WriteString("</user_message>\n")

	if len(request.History) != 0 {
		prompt.WriteString("\n<conversation_history>\n")
		for _, message := range request.History {
			prompt.WriteString("<message role=\"")
			prompt.WriteString(string(message.Role))
			prompt.WriteString("\">")
			prompt.WriteString(message.Content)
			prompt.WriteString("</message>\n")
		}
		prompt.WriteString("</conversation_history>\n")
	}

	return genai.NewContentFromText(prompt.String(), genai.RoleUser)
}
