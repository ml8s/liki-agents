package audit

import "context"

// Recorder persists immutable audit facts. Implementations must treat Record
// as append-only: an event must never overwrite or remove a prior event.
type Recorder interface {
	Record(ctx context.Context, event *Event) error
}
