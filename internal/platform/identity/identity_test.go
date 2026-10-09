package identity

import (
	"context"
	"testing"
)

func TestIdentityContextRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := WithIdentity(context.Background(), Identity{UserID: "user_1"})
	userID, ok := UserIDFromContext(ctx)
	if !ok || userID != "user_1" {
		t.Fatalf("userID = %q, ok=%v", userID, ok)
	}
}

func TestMissingIdentity(t *testing.T) {
	t.Parallel()
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("empty context unexpectedly has identity")
	}
}
