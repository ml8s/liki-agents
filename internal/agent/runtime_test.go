package agent_test

import (
	"errors"
	"testing"
	"time"

	agent "github.com/liki/liki-agent/internal/agent"
	"github.com/liki/liki-agent/internal/domain"
)

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
				AllowedTools: []string{"bazi_chart"},
				EngineMCPURL: "http://127.0.0.1:1/mcp",
			},
			expected: "llm_model_missing",
		},
		{
			name: "missing tools",
			config: agent.Config{
				Model:        "test-model",
				EngineMCPURL: "http://127.0.0.1:1/mcp",
			},
			expected: "engine_tools_empty",
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
