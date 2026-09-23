// Package agui exposes the same ADK runtime as AG-UI over its official SSE
// event encoding. The browser-facing product gateway remains the trust boundary.
package agui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	aguisse "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/liki/liki-agent/internal/agent"
	"github.com/liki/liki-agent/internal/domain"
	"github.com/liki/liki-agent/internal/observability"
	"github.com/liki/liki-agent/internal/platform/identity"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const Path = "/ag-ui"

type Config struct {
	RunTimeout time.Duration
	Metrics    observability.ProtocolMetrics
}

// Handler is the AG-UI protocol adapter.
type Handler struct {
	runtime    runtime
	runTimeout time.Duration
	metrics    observability.ProtocolMetrics
	writer     *aguisse.SSEWriter
}

// runtime is the single ADK runtime contract consumed by this protocol adapter.
type runtime interface {
	Run(ctx context.Context, request agent.RunRequest, observe func(*session.Event) error) (agent.RunResult, error)
}

// New creates the standard AG-UI HTTP handler.
func New(runtime runtime, config Config) (*Handler, error) {
	if runtime == nil {
		return nil, fmt.Errorf("adk runtime is required")
	}
	if config.RunTimeout <= 0 {
		config.RunTimeout = 10 * time.Minute
	}
	return &Handler{
		runtime:    runtime,
		runTimeout: config.RunTimeout,
		metrics:    config.Metrics,
		writer:     aguisse.NewSSEWriter(),
	}, nil
}

// ServeHTTP implements POST /ag-ui and AG-UI SSE responses.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeProtocolError(w, http.StatusMethodNotAllowed, domain.CodeMethodNotAllowed, "AG-UI requires POST")
		return
	}

	var input aguitypes.RunAgentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeProtocolError(w, http.StatusBadRequest, domain.CodeInvalidAGUIRequest, "invalid AG-UI request body")
		return
	}

	caller, ok := identity.UserIDFromContext(r.Context())
	if !ok || strings.TrimSpace(caller) == "" {
		writeProtocolError(w, http.StatusUnauthorized, domain.CodeIdentityRequired, "verified user identity required")
		return
	}

	threadID := strings.TrimSpace(input.ThreadID)
	if threadID == "" {
		threadID = aguievents.GenerateThreadID()
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		runID = aguievents.GenerateRunID()
	}
	if err := validateTextChatProfile(input); err != nil {
		writeProtocolError(w, http.StatusUnprocessableEntity, domain.CodeUnsupportedAGUIFeature, err.Error())
		return
	}
	content, history, err := lastUserConversation(input.Messages)
	if err != nil {
		writeProtocolError(w, http.StatusUnprocessableEntity, domain.CodeInvalidAGUIMessage, err.Error())
		return
	}

	if h.metrics != nil {
		h.metrics.StreamOpened("ag_ui")
		defer h.metrics.StreamClosed("ag_ui")
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	emit := func(event aguievents.Event) error {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("AG-UI event is invalid: %w", err)
		}
		return h.writer.WriteEvent(r.Context(), w, event)
	}
	if err := emit(aguievents.NewRunStartedEvent(threadID, runID)); err != nil {
		return
	}

	streamer := &stream{
		threadID:    threadID,
		runID:       runID,
		messageID:   runID + ":assistant",
		activeTools: make(map[string]string),
	}
	spec := agent.RunRequest{
		RunID:       runID,
		ThreadID:    threadID,
		UserID:      caller,
		Product:     forwardedString(input.ForwardedProps, "product", "liki-agent"),
		Locale:      forwardedString(input.ForwardedProps, "locale", "zh-CN"),
		UserMessage: content,
		History:     history,
	}
	runCtx, cancelRun := context.WithTimeout(r.Context(), h.runTimeout)
	defer cancelRun()
	result, runErr := h.runtime.Run(runCtx, spec, func(event *session.Event) error {
		return streamer.consume(event, emit)
	})
	if runErr == nil {
		runErr = streamer.finish(result.FinalContent, emit)
	}
	if runErr == nil {
		finished := aguievents.NewRunFinishedEvent(threadID, runID)
		finished.Result = map[string]any{
			"answer":          result.FinalContent,
			"expert_opinions": result.ExpertOpinions,
			"source_tools":    result.SourceTools,
		}
		runErr = emit(finished)
	}
	if runErr != nil && !errors.Is(runErr, context.Canceled) {
		_ = emit(aguievents.NewRunErrorEvent(publicErrorMessage(runErr)))
	}
}

// stream converts framework-native execution facts into official AG-UI events.
// It owns no alternative runtime or business state.
type stream struct {
	threadID    string
	runID       string
	messageID   string
	textStarted bool
	sawText     bool
	activeTools map[string]string
}

func (s *stream) consume(event *session.Event, emit func(aguievents.Event) error) error {
	if event == nil {
		return nil
	}
	for _, part := range event.Content.Parts {
		if part == nil || part.FunctionCall == nil {
			continue
		}
		call := part.FunctionCall
		callID := call.ID
		if callID == "" {
			callID = s.runID + ":" + call.Name
		}
		s.activeTools[callID] = call.Name
		if err := emit(aguievents.NewToolCallStartEvent(callID, call.Name)); err != nil {
			return err
		}
		args, err := json.Marshal(call.Args)
		if err != nil {
			args = []byte("{}")
		}
		delta := string(args)
		if delta == "" || delta == "null" {
			delta = "{}"
		}
		if err := emit(aguievents.NewToolCallArgsEvent(callID, delta)); err != nil {
			return err
		}
	}

	for _, part := range event.Content.Parts {
		if part == nil || part.FunctionResponse == nil {
			continue
		}
		response := part.FunctionResponse
		responseID := response.ID
		if responseID == "" {
			responseID = s.runID + ":" + response.Name
		}
		payload, err := json.Marshal(response.Response)
		if err != nil || len(payload) == 0 || string(payload) == "null" {
			payload = []byte("{}")
		}
		if err := emit(aguievents.NewToolCallResultEvent(s.messageID, responseID, string(payload))); err != nil {
			return err
		}
		if err := emit(aguievents.NewToolCallEndEvent(responseID)); err != nil {
			return err
		}
		delete(s.activeTools, responseID)
	}

	text := textFromParts(event.Content.Parts)
	if event.Partial && text != "" {
		if err := s.startText(emit); err != nil {
			return err
		}
		return emit(aguievents.NewTextMessageContentEvent(s.messageID, text))
	}
	if event.IsFinalResponse() && text != "" && !s.sawText {
		if err := s.startText(emit); err != nil {
			return err
		}
		if err := emit(aguievents.NewTextMessageContentEvent(s.messageID, text)); err != nil {
			return err
		}
	}
	if event.IsFinalResponse() && s.textStarted {
		s.textStarted = false
		return emit(aguievents.NewTextMessageEndEvent(s.messageID))
	}
	return nil
}

// finish closes an active message and synthesizes the standard text lifecycle
// when the model returned only a structured final answer.
func (s *stream) finish(content string, emit func(aguievents.Event) error) error {
	if s.textStarted {
		s.textStarted = false
		if err := emit(aguievents.NewTextMessageEndEvent(s.messageID)); err != nil {
			return err
		}
	}
	if s.sawText || strings.TrimSpace(content) == "" {
		return nil
	}
	if err := s.startText(emit); err != nil {
		return err
	}
	if err := emit(aguievents.NewTextMessageContentEvent(s.messageID, content)); err != nil {
		return err
	}
	s.textStarted = false
	return emit(aguievents.NewTextMessageEndEvent(s.messageID))
}

func (s *stream) startText(emit func(aguievents.Event) error) error {
	if s.textStarted {
		return nil
	}
	s.textStarted = true
	s.sawText = true
	return emit(aguievents.NewTextMessageStartEvent(s.messageID, aguievents.WithRole(string(aguitypes.RoleAssistant))))
}

// validateTextChatProfile rejects capabilities this runtime does not consume.
// Explicit rejection prevents silent protocol data loss at the adapter edge.
func validateTextChatProfile(input aguitypes.RunAgentInput) error {
	if input.ParentRunID != nil {
		return fmt.Errorf("parent runs are outside the AG-UI text-chat profile")
	}
	if input.State != nil {
		return fmt.Errorf("client state is outside the AG-UI text-chat profile")
	}
	if len(input.Tools) != 0 {
		return fmt.Errorf("client tools are outside the AG-UI text-chat profile")
	}
	if len(input.Context) != 0 {
		return fmt.Errorf("client context is outside the AG-UI text-chat profile")
	}
	if len(input.Resume) != 0 {
		return fmt.Errorf("resume is outside the AG-UI text-chat profile")
	}
	for _, message := range input.Messages {
		switch message.Role {
		case aguitypes.RoleUser, aguitypes.RoleAssistant:
		default:
			return fmt.Errorf("message role %q is outside the AG-UI text-chat profile", message.Role)
		}
		if len(message.ToolCalls) != 0 || message.ToolCallID != "" || message.ActivityType != "" || message.SubagentRunID != "" {
			return fmt.Errorf("tool or activity messages are outside the AG-UI text-chat profile")
		}
	}
	return nil
}

func lastUserConversation(messages []aguitypes.Message) (string, []agent.Message, error) {
	lastIndex := -1
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role == aguitypes.RoleUser {
			lastIndex = index
			break
		}
	}
	if lastIndex < 0 {
		return "", nil, fmt.Errorf("AG-UI request must contain a user message")
	}
	content, err := conversationText(messages[lastIndex].Content)
	if err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(content) == "" {
		return "", nil, fmt.Errorf("the latest user message is empty")
	}

	history := make([]agent.Message, 0, len(messages)-1)
	for index, message := range messages {
		if index == lastIndex || message.Role != aguitypes.RoleUser && message.Role != aguitypes.RoleAssistant {
			continue
		}
		text, err := conversationText(message.Content)
		if err != nil {
			return "", nil, err
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		role := agent.RoleUser
		if message.Role == aguitypes.RoleAssistant {
			role = agent.RoleAssistant
		}
		history = append(history, agent.Message{Role: role, Content: text})
	}
	return content, history, nil
}

func conversationText(content any) (string, error) {
	switch value := content.(type) {
	case string:
		return value, nil
	case []aguitypes.InputContent:
		var result strings.Builder
		for _, fragment := range value {
			if fragment.Type != aguitypes.InputContentTypeText {
				return "", fmt.Errorf("input content type %q is outside the AG-UI text-chat profile", fragment.Type)
			}
			result.WriteString(fragment.Text)
		}
		return result.String(), nil
	case []any:
		var result strings.Builder
		for _, item := range value {
			fragment, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if kind, ok := fragment["type"].(string); ok && kind != aguitypes.InputContentTypeText {
				return "", fmt.Errorf("input content type %q is outside the AG-UI text-chat profile", kind)
			}
			if text, ok := fragment["text"].(string); ok {
				result.WriteString(text)
			}
		}
		return result.String(), nil
	default:
		return "", fmt.Errorf("message content is outside the AG-UI text-chat profile")
	}
}

func textFromParts(parts []*genai.Part) string {
	if len(parts) == 0 {
		return ""
	}
	var result strings.Builder
	for _, part := range parts {
		if part != nil && !part.Thought {
			result.WriteString(part.Text)
		}
	}
	return result.String()
}

func forwardedString(value any, key, fallback string) string {
	object, ok := value.(map[string]any)
	if !ok {
		return fallback
	}
	if text, ok := object[key].(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

func publicErrorMessage(err error) string {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) && domainErr.Message != "" {
		return domainErr.Message
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "agent run exceeded its deadline"
	}
	return "agent run failed"
}

func writeProtocolError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": code, "message": message}})
}
