package agent_test

import (
	"context"
	"errors"
	"testing"
	"time"

	agent "github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/domain"
)

type nopAuditRecorder struct{}

func (nopAuditRecorder) Record(context.Context, *audit.Event) error { return nil }

func fixedNow() time.Time {
	return time.Date(2026, 9, 22, 1, 2, 3, 0, time.UTC)
}

func TestNewRuntimeValidatesContract(t *testing.T) {
	tests := []struct {
		name     string
		config   agent.Config
		expected string
	}{
		{
			name: "missing model",
			config: agent.Config{
				Deployment:    agent.NewTestDeployment(t),
				AuditRecorder: nopAuditRecorder{},
				EngineMCPURL:  "http://127.0.0.1:1/mcp",
			},
			expected: "llm_model_missing",
		},
		{
			name: "invalid structured output capability",
			config: agent.Config{
				Model:            "test-model",
				Deployment:       agent.NewTestDeployment(t),
				AuditRecorder:    nopAuditRecorder{},
				EngineMCPURL:     "http://127.0.0.1:1/mcp",
				StructuredOutput: "yaml",
			},
			expected: "structured_output_capability_invalid",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := agent.NewRuntime(test.config)
			var domainErr *domain.Error
			if !errors.As(err, &domainErr) || domainErr.Code != test.expected {
				t.Fatalf("NewRuntime() error = %v, want %s", err, test.expected)
			}
		})
	}
}
