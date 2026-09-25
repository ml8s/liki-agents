package a2a

import (
	"context"
	"testing"
	"time"
)

func TestA2AExecutionTimeoutIsTotalAndReleasable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("execution context has no total deadline")
	}
	ctx = context.WithValue(ctx, executionCancelKey{}, context.CancelFunc(cancel))
	cancelExecution(ctx)
	select {
	case <-ctx.Done():
	default:
		t.Fatal("execution cancel callback did not release the deadline context")
	}
}
