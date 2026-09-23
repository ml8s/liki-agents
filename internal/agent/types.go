package agent

import (
	"context"

	"github.com/liki/liki-agent/internal/domain"
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
	Product     string
	Locale      string
	UserMessage string
	History     []Message
}

type RunResult struct {
	FinalContent   string
	ExpertOpinions []domain.ExpertOpinion
	SourceTools    []string
	Model          string
}

// StructuredOutputStateKey is the ADK session-state key that holds the parsed
// structured model output. ADK clears Event.Output before yielding events, so
// this state key is the framework contract for structured output.
const StructuredOutputStateKey = structuredOutputStateKey

type LLMCallRecorder interface {
	Start(ctx context.Context, call *domain.LLMCall) error
	Finish(ctx context.Context, call *domain.LLMCall) error
}

type Metrics interface {
	ObserveLLMCall(model, status string, promptTokens, completionTokens, totalTokens int64)
}
