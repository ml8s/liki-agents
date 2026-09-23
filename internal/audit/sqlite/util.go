package sqlite

import (
	"context"
	"errors"

	"github.com/liki/liki-agent/internal/domain"
	"gorm.io/gorm"
)

type contextKey struct{}

func sessionFromContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(contextKey{}).(*gorm.DB); ok {
		return tx
	}
	return db.WithContext(ctx)
}

func mapError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return domain.NewError(domain.CodeAuditStoreFailed, "LLM audit store operation failed", err)
}
