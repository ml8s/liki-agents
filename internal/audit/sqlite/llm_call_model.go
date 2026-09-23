package sqlite

import (
	"time"

	"github.com/liki/liki-agent/internal/domain"
)

type llmCallModel struct {
	ID               string    `gorm:"column:id;primaryKey"`
	RunID            string    `gorm:"column:run_id;index"`
	ThreadID         string    `gorm:"column:thread_id;index"`
	UserID           string    `gorm:"column:user_id"`
	Product          string    `gorm:"column:product"`
	AgentName        string    `gorm:"column:agent_name"`
	Model            string    `gorm:"column:model;index"`
	Provider         string    `gorm:"column:provider"`
	Status           string    `gorm:"column:status"`
	PromptTokens     int64     `gorm:"column:prompt_tokens"`
	CompletionTokens int64     `gorm:"column:completion_tokens"`
	ThoughtTokens    int64     `gorm:"column:thought_tokens"`
	TotalTokens      int64     `gorm:"column:total_tokens"`
	DurationMS       int64     `gorm:"column:duration_ms"`
	ErrorCode        string    `gorm:"column:error_code"`
	ErrorMessage     string    `gorm:"column:error_message"`
	GraphVersion     string    `gorm:"column:graph_version"`
	ContractVersion  string    `gorm:"column:contract_version"`
	PromptVersion    string    `gorm:"column:prompt_version"`
	PolicyVersion    string    `gorm:"column:policy_version"`
	StartedAt        time.Time `gorm:"column:started_at"`
	FinishedAt       time.Time `gorm:"column:finished_at"`
}

func (llmCallModel) TableName() string { return "agent_llm_calls" }

func newLLMCallModel(call *domain.LLMCall) llmCallModel {
	return llmCallModel{
		ID:               call.ID,
		RunID:            string(call.RunID),
		ThreadID:         string(call.ThreadID),
		UserID:           call.UserID,
		Product:          call.Product,
		AgentName:        call.AgentName,
		Model:            call.Model,
		Provider:         call.Provider,
		Status:           string(call.Status),
		PromptTokens:     call.PromptTokens,
		CompletionTokens: call.CompletionTokens,
		ThoughtTokens:    call.ThoughtTokens,
		TotalTokens:      call.TotalTokens,
		DurationMS:       call.DurationMS,
		ErrorCode:        call.ErrorCode,
		ErrorMessage:     call.ErrorMessage,
		GraphVersion:     call.GraphVersion,
		ContractVersion:  call.ContractVersion,
		PromptVersion:    call.PromptVersion,
		PolicyVersion:    call.PolicyVersion,
		StartedAt:        call.StartedAt,
		FinishedAt:       call.FinishedAt,
	}
}

func (m llmCallModel) llmCall() *domain.LLMCall {
	return &domain.LLMCall{
		ID:               m.ID,
		RunID:            domain.ID(m.RunID),
		ThreadID:         domain.ID(m.ThreadID),
		UserID:           m.UserID,
		Product:          m.Product,
		AgentName:        m.AgentName,
		Model:            m.Model,
		Provider:         m.Provider,
		Status:           domain.LLMCallStatus(m.Status),
		PromptTokens:     m.PromptTokens,
		CompletionTokens: m.CompletionTokens,
		ThoughtTokens:    m.ThoughtTokens,
		TotalTokens:      m.TotalTokens,
		DurationMS:       m.DurationMS,
		ErrorCode:        m.ErrorCode,
		ErrorMessage:     m.ErrorMessage,
		GraphVersion:     m.GraphVersion,
		ContractVersion:  m.ContractVersion,
		PromptVersion:    m.PromptVersion,
		PolicyVersion:    m.PolicyVersion,
		StartedAt:        m.StartedAt,
		FinishedAt:       m.FinishedAt,
	}
}
