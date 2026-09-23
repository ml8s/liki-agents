package domain

import (
	"errors"
	"strings"
	"time"
)

type LLMCallStatus string

// ID is a logical cross-service identifier. The agent does not own foreign
// tables; IDs are opaque audit correlation values.
type ID string

const (
	LLMCallRunning   LLMCallStatus = "running"
	LLMCallCompleted LLMCallStatus = "completed"
	LLMCallFailed    LLMCallStatus = "failed"
)

type LLMTokenUsage struct {
	PromptTokens     int64
	CompletionTokens int64
	ThoughtTokens    int64
	TotalTokens      int64
}

type LLMCall struct {
	ID               string
	RunID            ID
	ThreadID         ID
	UserID           string
	AgentName        string
	Model            string
	Provider         string
	Status           LLMCallStatus
	PromptTokens     int64
	CompletionTokens int64
	ThoughtTokens    int64
	TotalTokens      int64
	DurationMS       int64
	Product          string
	ErrorCode        string
	ErrorMessage     string
	GraphVersion     string
	ContractVersion  string
	PromptVersion    string
	PolicyVersion    string
	StartedAt        time.Time
	FinishedAt       time.Time
}

func (c *LLMCall) Normalize() {
	c.ID = strings.TrimSpace(c.ID)
	c.AgentName = strings.TrimSpace(c.AgentName)
	c.Model = strings.TrimSpace(c.Model)
	c.ErrorCode = strings.TrimSpace(c.ErrorCode)
}

func (c *LLMCall) Validate() error {
	switch {
	case c.ID == "":
		return NewError(CodeLLMCallIDRequired, "llm call id is required", ErrInvalidInput)
	case c.RunID == "":
		return NewError(CodeRunIDRequired, "run_id is required", ErrInvalidInput)
	case c.ThreadID == "":
		return NewError(CodeThreadIDRequired, "thread_id is required", ErrInvalidInput)
	case c.UserID == "":
		return NewError(CodeUserIDRequired, "user_id is required", ErrInvalidInput)
	case c.AgentName == "":
		return NewError(CodeAgentNameRequired, "agent_name is required", ErrInvalidInput)
	case c.Model == "":
		return NewError(CodeModelRequired, "model is required", ErrInvalidInput)
	case c.StartedAt.IsZero():
		return NewError(CodeLLMCallStartedAtRequired, "llm call start time is required", ErrInvalidInput)
	}
	return nil
}

func (c *LLMCall) Complete(usage LLMTokenUsage, finishedAt time.Time) {
	c.Status = LLMCallCompleted
	c.PromptTokens = usage.PromptTokens
	c.CompletionTokens = usage.CompletionTokens
	c.ThoughtTokens = usage.ThoughtTokens
	c.TotalTokens = usage.TotalTokens
	c.FinishedAt = finishedAt
	c.DurationMS = finishedAt.Sub(c.StartedAt).Milliseconds()
}

func (c *LLMCall) Fail(err error, finishedAt time.Time) {
	c.Status = LLMCallFailed
	c.FinishedAt = finishedAt
	c.DurationMS = finishedAt.Sub(c.StartedAt).Milliseconds()
	var domainErr *Error
	if errors.As(err, &domainErr) {
		c.ErrorCode = domainErr.Code
		c.ErrorMessage = domainErr.Message
		return
	}
	c.ErrorCode = CodeLLMCallFailed
	c.ErrorMessage = "LLM call failed"
}
