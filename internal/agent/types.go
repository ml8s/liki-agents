package agent

import (
	"encoding/json"
	"time"
	"unicode"

	"github.com/ml8s/liki-agents/internal/domain"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

const (
	// MaxIdentifierLength bounds untrusted identifiers before they reach ADK,
	// audit storage, tracing, and in-memory lifecycle maps.
	MaxIdentifierLength = 128
	// MaxHistoryMessages prevents a protocol history from becoming an
	// unbounded reconstructed prompt.
	MaxHistoryMessages = 100
)

type Message struct {
	Role    Role
	Content string
}

type RunRequest struct {
	RunID       string
	ThreadID    string
	UserID      string
	Protocol    string
	UserMessage string
	History     []Message
	Context     json.RawMessage
}

type RunResult struct {
	Definition DefinitionRef
	Output     json.RawMessage
	Text       string
}

// StructuredOutputStateKey returns the ADK session-state key that holds an
// Agent's parsed structured model output. Keys are Agent-scoped so delegated
// structured Agents cannot overwrite the entrypoint result.
func StructuredOutputStateKey(agentName string) string {
	return structuredOutputStateKeyPrefix + agentName
}

type Metrics interface {
	ObserveLLMCall(model, status string, usage domain.LLMTokenUsage)
	ObserveToolCall(agent, tool, status string, duration time.Duration)
	ObserveAgentDelegation(caller, target, status string, duration time.Duration)
}

// ValidIdentifier accepts bounded printable identifiers while preserving
// provider-specific identifier alphabets.
func ValidIdentifier(value string) bool {
	if value == "" || len(value) > MaxIdentifierLength {
		return false
	}
	for _, char := range value {
		if !unicode.IsPrint(char) {
			return false
		}
	}
	return true
}
