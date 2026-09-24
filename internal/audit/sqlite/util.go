package sqlite

import (
	"context"

	"gorm.io/gorm"
)

type contextKey struct{}

func sessionFromContext(ctx context.Context, db *gorm.DB) *gorm.DB {
	if tx, ok := ctx.Value(contextKey{}).(*gorm.DB); ok {
		return tx
	}
	return db.WithContext(ctx)
}
