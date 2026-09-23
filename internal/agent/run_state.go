package agent

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type runState struct {
	request         RunRequest
	now             func() time.Time
	structured      bool
	analysis        *structuredAnalysis
	final           strings.Builder
	sawFinal        bool
	toolsSeen       []string
	nextToolCall    int
	activeToolCalls map[string]toolCallRuntime
	lastToolCalls   map[string]toolCallRuntime
}

type toolCallRuntime struct {
	id        string
	startedAt time.Time
}

func (s *runState) consume(event *session.Event) error {
	if event == nil {
		return nil
	}
	if event.Content != nil {
		for _, part := range event.Content.Parts {
			if part == nil {
				continue
			}
			if call := part.FunctionCall; call != nil {
				s.nextToolCall++
				callID := call.ID
				if callID == "" {
					callID = fmt.Sprintf("%s/%s/%d", s.request.RunID, call.Name, s.nextToolCall)
				}
				s.activeToolCalls[toolCallKey(callID, call.Name)] = toolCallRuntime{
					id:        callID,
					startedAt: s.now(),
				}
				s.lastToolCalls[call.Name] = s.activeToolCalls[toolCallKey(callID, call.Name)]
			}
			if response := part.FunctionResponse; response != nil {
				key := toolCallKey(response.ID, response.Name)
				runtimeCall, ok := s.activeToolCalls[key]
				delete(s.activeToolCalls, key)
				if !ok {
					runtimeCall, ok = s.lastToolCalls[response.Name]
				}
				if response.ID == "" {
					response.ID = runtimeCall.id
				}
				s.addTool(response.Name)
			}
		}
	}
	if event.Partial {
		return nil
	}
	if !event.IsFinalResponse() || s.sawFinal {
		return nil
	}
	text := textFromContent(event.Content)
	if text == "" {
		return nil
	}
	s.sawFinal = true
	s.final.WriteString(text)
	return nil
}

func (s *runState) addTool(name string) {
	for _, seen := range s.toolsSeen {
		if seen == name {
			return
		}
	}
	s.toolsSeen = append(s.toolsSeen, name)
}

func (s *runState) tools() []string {
	if len(s.toolsSeen) == 0 {
		return nil
	}
	result := make([]string, len(s.toolsSeen))
	copy(result, s.toolsSeen)
	return result
}

func buildUserContent(request RunRequest) *genai.Content {
	var prompt strings.Builder
	locale := request.Locale
	if locale == "" {
		locale = "zh-CN"
	}
	fmt.Fprintf(&prompt, "Locale: %s\n", locale)
	if len(request.History) > 0 {
		prompt.WriteString("Conversation history:\n")
		for _, message := range request.History {
			fmt.Fprintf(&prompt, "%s: %s\n", message.Role, message.Content)
		}
		prompt.WriteString("\n")
	}
	fmt.Fprintf(&prompt, "Question:\n%s", request.UserMessage)
	return genai.NewContentFromText(prompt.String(), genai.RoleUser)
}

func textFromContent(content *genai.Content) string {
	if content == nil {
		return ""
	}
	var result strings.Builder
	for _, part := range content.Parts {
		if part != nil && !part.Thought {
			result.WriteString(part.Text)
		}
	}
	return result.String()
}

func toolCallKey(id, tool string) string {
	return id + "\x00" + tool
}
