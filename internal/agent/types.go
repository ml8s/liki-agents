package agent

import (
	"encoding/json"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	Role    Role
	Content string
}

type RunRequest struct {
	RunID       string
	ThreadID    string
	UserID      string
	UserMessage string
	History     []Message
	Context     json.RawMessage
}

type RunResult struct {
	Definition DefinitionRef
	Output     json.RawMessage
	Text       string
}

// StructuredOutputStateKey is the ADK session-state key that holds the parsed
// structured model output. ADK clears Event.Output before yielding events, so
// this state key is the framework contract for structured output.
const StructuredOutputStateKey = structuredOutputStateKey

type Metrics interface {
	ObserveLLMCall(model, status string, usage domain.LLMTokenUsage)
	ObserveToolCall(agent, tool, status string, duration time.Duration)
	ObserveAgentDelegation(caller, target, status string, duration time.Duration)
}

type AuditRecorder = audit.Recorder
