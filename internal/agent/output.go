// Package agent: structured output contract and event visibility filtering.
// These functions define the model-to-protocol boundary for structured
// analysis; runtime.go owns execution, this file owns output shape.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const unstructuredFallbackConfidence = 0.35

type structuredAnalysis struct {
	Answer      string   `json:"answer"`
	Confidence  float64  `json:"confidence"`
	Topic       string   `json:"topic"`
	KeyFactors  []string `json:"key_factors"`
	Limitations []string `json:"limitations"`
}

func structuredOutputSchema() *genai.Schema {
	minimum, maximum := 0.0, 1.0
	stringItem := &genai.Schema{Type: genai.TypeString}
	return &genai.Schema{
		Title: "ExpertAnalysis",
		Type:  genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"answer": {
				Type:        genai.TypeString,
				MinLength:   ptrInt64(1),
				Description: "Complete user-facing analysis in the user's locale",
			},
			"confidence": {
				Type:    genai.TypeNumber,
				Minimum: &minimum,
				Maximum: &maximum,
			},
			"topic": {
				Type:        genai.TypeString,
				MinLength:   ptrInt64(1),
				Description: "Short canonical analysis topic",
			},
			"key_factors": {
				Type:        genai.TypeArray,
				Items:       stringItem,
				Description: "Deterministic or reasoning factors that support the answer",
			},
			"limitations": {
				Type:        genai.TypeArray,
				Items:       stringItem,
				Description: "Material limitations, uncertainty, and safety boundaries",
			},
		},
		Required:         []string{"answer", "confidence", "topic", "key_factors", "limitations"},
		PropertyOrdering: []string{"answer", "confidence", "topic", "key_factors", "limitations"},
	}
}

func ptrInt64(value int64) *int64 {
	return &value
}

func (s *runState) consumeStructuredOutput(value any) error {
	// Plain llmagent persists the raw model reply string under OutputKey;
	// the caller owns schema parsing. Other shapes (already-parsed maps) are
	// normalized through a JSON round trip.
	var analysis structuredAnalysis
	switch payload := value.(type) {
	case string:
		if err := json.Unmarshal([]byte(payload), &analysis); err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "decode structured agent output", err)
		}
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "decode structured agent output", err)
		}
		if err := json.Unmarshal(raw, &analysis); err != nil {
			return domain.NewError(domain.CodeStructuredOutputInvalid, "decode structured agent output", err)
		}
	}
	analysis.Answer = strings.TrimSpace(analysis.Answer)
	analysis.Topic = strings.TrimSpace(analysis.Topic)
	if analysis.Answer == "" {
		return domain.NewError(domain.CodeStructuredOutputEmpty, "structured agent output has no answer", nil)
	}
	if analysis.Topic == "" {
		analysis.Topic = "general"
	}
	s.analysis = &analysis
	return nil
}

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

func instruction(config Config) string {
	return fmt.Sprintf(`You are %s, a disciplined destiny-analysis expert.
Use the allowlisted Engine tools for every deterministic chart claim.
Never invent chart facts. Preserve uncertainty and avoid medical, legal, or investment directives.
Return the final result through the configured structured output contract.
The answer field must contain the complete user-facing response, not JSON.`, config.ExpertName)
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
