package domain

import (
	"testing"
	"time"
)

func TestExpertOpinionValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ExpertOpinion)
		wantErr bool
	}{
		{name: "valid", mutate: func(*ExpertOpinion) {}},
		{name: "missing expert", mutate: func(opinion *ExpertOpinion) { opinion.Expert = "" }, wantErr: true},
		{name: "missing system", mutate: func(opinion *ExpertOpinion) { opinion.System = "" }, wantErr: true},
		{name: "missing conclusion", mutate: func(opinion *ExpertOpinion) { opinion.Conclusion = "" }, wantErr: true},
		{name: "confidence too low", mutate: func(opinion *ExpertOpinion) { opinion.Confidence = -0.1 }, wantErr: true},
		{name: "confidence too high", mutate: func(opinion *ExpertOpinion) { opinion.Confidence = 1.1 }, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			opinion := ExpertOpinion{
				Expert:            "chief_analyst",
				System:            "chief",
				Topic:             "general",
				Conclusion:        "grounded answer",
				Confidence:        0.72,
				SupportingFactors: []string{"seven killings is heavy"},
				Limitations:       []string{"not financial advice"},
				SourceTools:       []string{"bazi_chart"},
				Metadata:          map[string]any{"prompt_version": "chief-analysis-v1"},
				CreatedAt:         time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC),
			}
			test.mutate(&opinion)
			err := opinion.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, want error %v", err, test.wantErr)
			}
		})
	}
}
