// Structured output contract and event visibility filtering define the
// model-to-protocol boundary; runtime.go owns execution, this file owns shape.
package agent

import (
	"context"
	"errors"

	"github.com/ml8s/liki-agents/internal/domain"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func visibleEvent(event *session.Event) *session.Event {
	// ADK emits partial function-call chunks and then the final call with the
	// same identity. The structured graph has no user-facing partial text:
	// model text is either protocol payload or tool I/O. Observers therefore
	// receive only finalized facts, which prevents duplicate tool-call events
	// at the protocol boundary.
	if event == nil || event.Partial {
		return nil
	}
	if event.Content == nil {
		return event
	}
	hasPayloadText := false
	kept := make([]*genai.Part, 0, len(event.Content.Parts))
	for _, part := range event.Content.Parts {
		if part == nil {
			continue
		}
		if part.FunctionCall == nil && part.FunctionResponse == nil {
			hasPayloadText = true
			continue
		}
		kept = append(kept, part)
	}
	if !hasPayloadText {
		return event
	}
	if len(kept) == 0 {
		return nil
	}
	clone := *event
	clone.Content = &genai.Content{Role: event.Content.Role, Parts: kept}
	return &clone
}

func runtimeError(err error) error {
	if err == nil {
		return nil
	}
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		return domainErr
	}
	if errors.Is(err, context.Canceled) {
		return domain.NewError(domain.CodeRuntimeCancelled, "agent run was cancelled", err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return domain.NewError(domain.CodeRuntimeTimeout, "agent run exceeded its deadline", err)
	}
	return domain.NewError(domain.CodeRuntimeFailed, "agent runtime failed", err)
}
