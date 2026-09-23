package domain

import "time"

type ExpertOpinion struct {
	Expert            string         `json:"expert"`
	System            string         `json:"system"`
	School            string         `json:"school,omitempty"`
	Topic             string         `json:"topic,omitempty"`
	Conclusion        string         `json:"conclusion"`
	Confidence        float64        `json:"confidence"`
	SupportingFactors []string       `json:"supporting_factors,omitempty"`
	Limitations       []string       `json:"limitations,omitempty"`
	SourceTools       []string       `json:"source_tools,omitempty"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
}

func (o *ExpertOpinion) Validate() error {
	if o.Expert == "" {
		return NewError(CodeExpertRequired, "expert is required", ErrInvalidInput)
	}
	if o.System == "" {
		return NewError(CodeSystemRequired, "system is required", ErrInvalidInput)
	}
	if o.Conclusion == "" {
		return NewError(CodeConclusionRequired, "conclusion is required", ErrInvalidInput)
	}
	if o.Confidence < 0 || o.Confidence > 1 {
		return NewError(CodeInvalidConfidence, "confidence must be between 0 and 1", ErrInvalidInput)
	}
	return nil
}
