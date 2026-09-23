// Package identity carries the verified caller identity across service
// boundaries. liki-web authenticates the end user; liki-agent trusts this
// kernel identity only after its own service authentication.
package identity

import (
	"context"
	"strings"
)

type Identity struct {
	UserID string
	Scopes []string
}

func (i Identity) IsZero() bool {
	return strings.TrimSpace(i.UserID) == ""
}

type contextKey struct{}

func WithIdentity(ctx context.Context, subject Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, subject)
}

func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey{}).(Identity)
	if !ok || identity.IsZero() {
		return Identity{}, false
	}
	return identity, true
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	identity, ok := FromContext(ctx)
	if !ok {
		return "", false
	}
	return identity.UserID, true
}
