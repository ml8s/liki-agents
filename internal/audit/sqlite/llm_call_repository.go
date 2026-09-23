package sqlite

import (
	"context"

	"github.com/liki/liki-agent/internal/domain"
	"gorm.io/gorm"
)

type LLMCallRepository struct {
	db *gorm.DB
}

func NewLLMCallRepository(db *gorm.DB) *LLMCallRepository {
	return &LLMCallRepository{db: db}
}

func (r *LLMCallRepository) Start(ctx context.Context, call *domain.LLMCall) error {
	model := newLLMCallModel(call)
	return mapError(sessionFromContext(ctx, r.db).Create(&model).Error)
}

func (r *LLMCallRepository) Finish(ctx context.Context, call *domain.LLMCall) error {
	model := newLLMCallModel(call)
	result := sessionFromContext(ctx, r.db).Model(&llmCallModel{}).
		Where("id = ?", call.ID).
		Updates(map[string]any{
			"status":            model.Status,
			"prompt_tokens":     model.PromptTokens,
			"completion_tokens": model.CompletionTokens,
			"thought_tokens":    model.ThoughtTokens,
			"total_tokens":      model.TotalTokens,
			"duration_ms":       model.DurationMS,
			"error_code":        model.ErrorCode,
			"error_message":     model.ErrorMessage,
			"finished_at":       model.FinishedAt,
		})
	if result.Error != nil {
		return mapError(result.Error)
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *LLMCallRepository) Get(ctx context.Context, id string) (*domain.LLMCall, error) {
	var model llmCallModel
	if err := sessionFromContext(ctx, r.db).First(&model, "id = ?", id).Error; err != nil {
		return nil, mapError(err)
	}
	return model.llmCall(), nil
}

func (r *LLMCallRepository) ListByRun(ctx context.Context, runID domain.ID) ([]domain.LLMCall, error) {
	var models []llmCallModel
	err := sessionFromContext(ctx, r.db).
		Order("started_at ASC, id ASC").
		Find(&models, "run_id = ?", string(runID)).Error
	if err != nil {
		return nil, mapError(err)
	}
	calls := make([]domain.LLMCall, 0, len(models))
	for _, model := range models {
		calls = append(calls, *model.llmCall())
	}
	return calls, nil
}
