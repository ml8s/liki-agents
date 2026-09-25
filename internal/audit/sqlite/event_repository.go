package sqlite

import (
	"context"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
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
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		var existing auditEventModel
		findErr := r.db.WithContext(ctx).Where("id = ?", event.ID).First(&existing).Error
		if findErr == nil && existing == model {
			// A retry after a committed-but-unacknowledged append is idempotent.
			return nil
		}
		if findErr == nil {
			return audit.NewError(audit.CodeAuditEventConflict, "audit event id already exists", err)
		}
		return audit.NewError(audit.CodeAuditAppendFailed, "append audit event", err)
	}
	return nil
}

func (r *AuditEventRepository) RunExists(ctx context.Context, runID domain.ID) (bool, error) {
	var count int64
	if err := r.db.WithContext(ctx).
		Model(&auditEventModel{}).
		Where("run_id = ?", string(runID)).
		Limit(1).
		Count(&count).Error; err != nil {
		return false, audit.NewError(audit.CodeAuditAppendFailed, "check run existence", err)
	}
	return count > 0, nil
}
