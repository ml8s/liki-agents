package audit

import "context"

import "github.com/ml8s/liki-agents/internal/domain"

// Recorder persists immutable audit facts. Implementations must treat Record
// as append-only: an event must never overwrite or remove a prior event.
type Recorder interface {
	Record(ctx context.Context, event *Event) error
}

// RunExistenceChecker is an optional recorder capability for durable RunID
// idempotency after process restart or in-memory registry eviction.
type RunExistenceChecker interface {
	RunExists(ctx context.Context, runID domain.ID) (bool, error)
}
