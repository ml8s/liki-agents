package sqlite

import (
	"context"

	"github.com/ml8s/liki-agents/internal/audit"
	"gorm.io/gorm"
)

type AuditEventRepository struct {
	db *gorm.DB
}

func NewAuditEventRepository(db *gorm.DB) *AuditEventRepository {
	return &AuditEventRepository{db: db}
}

func (r *AuditEventRepository) Record(ctx context.Context, event *audit.Event) error {
	if event == nil {
		return audit.NewError(audit.CodeAuditAppendFailed, "audit event is required", nil)
	}
	if err := event.Validate(); err != nil {
		return err
	}
	model, err := newAuditEventModel(event)
	if err != nil {
		return err
	}
	if err := sessionFromContext(ctx, r.db).Create(&model).Error; err != nil {
		return audit.NewError(audit.CodeAuditAppendFailed, "append audit event", err)
	}
	return nil
}
