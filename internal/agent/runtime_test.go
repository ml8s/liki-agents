package agent_test

import (
	"context"
	"errors"
	"strings"
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
			},
			expected: "llm_model_missing",
		},
		{
			name: "invalid structured output capability",
			config: agent.Config{
				Model:            "test-model",
				Deployment:       agent.NewTestDeployment(t),
				AuditRecorder:    nopAuditRecorder{},
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

func TestNewRuntimePinsDeploymentDigest(t *testing.T) {
	deployment := agent.NewTestDeployment(t)
	t.Setenv("TEST_MCP_ENDPOINT", "http://127.0.0.1:9/mcp")
	validDigest := deployment.Digest
	tests := []struct {
		name   string
		digest string
		code   string
	}{
		{name: "invalid format", digest: "sha256:not-a-digest", code: domain.CodeDeploymentDigestInvalid},
		{name: "mismatch", digest: "sha256:" + strings.Repeat("0", 64), code: domain.CodeDeploymentDigestMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := agent.NewRuntime(agent.Config{
				Model:            "test-model",
				ModelAPIKey:      "test-key",
				Deployment:       deployment,
				DeploymentDigest: test.digest,
				AuditRecorder:    nopAuditRecorder{},
			})
			var domainErr *domain.Error
			if !errors.As(err, &domainErr) || domainErr.Code != test.code {
				t.Fatalf("NewRuntime() error = %v, want %s", err, test.code)
			}
		})
	}

	runtime, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		ModelAPIKey:      "test-key",
		Deployment:       deployment,
		DeploymentDigest: validDigest,
		StructuredOutput: agent.StructuredOutputJSONSchema,
		AuditRecorder:    nopAuditRecorder{},
	})
	if err != nil {
		t.Fatalf("NewRuntime(pinned digest) error = %v", err)
	}
	if runtime == nil {
		t.Fatal("NewRuntime(pinned digest) returned nil")
	}
}

func TestRuntimeExposesImmutableDeploymentViews(t *testing.T) {
	runtime, _ := newAuditRuntime(t)
	entrypoint := runtime.Entrypoint()
	entrypoint.Name = "mutated"
	entrypoint.Tools.Allow["test"] = []string{"unauthorized"}
	mutatedDeployment := runtime.Deployment()
	mutatedDeployment.Metadata.Name = "mutated-deployment"

	unchangedEntrypoint := runtime.Entrypoint()
	unchangedDeployment := runtime.Deployment()
	if unchangedEntrypoint.Name == "mutated" ||
		len(unchangedEntrypoint.Tools.Allow["test"]) != 1 ||
		unchangedEntrypoint.Tools.Allow["test"][0] != "test_tool" ||
		unchangedDeployment.Metadata.Name == "mutated-deployment" {
		t.Fatalf("runtime view reflected protocol mutation: %+v", unchangedEntrypoint)
	}
}

func TestNewRuntimeRequiresMCPEndpointEnvironment(t *testing.T) {
	deployment := agent.NewTestDeployment(t)
	t.Setenv("TEST_MCP_ENDPOINT", "")
	_, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		Deployment:       deployment,
		StructuredOutput: agent.StructuredOutputJSONSchema,
		AuditRecorder:    nopAuditRecorder{},
	})
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeMCPEndpointEnvMissing {
		t.Fatalf("NewRuntime() error = %v, want mcp_endpoint_env_missing", err)
	}
}

func TestNewRuntimeRejectsInvalidMCPEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{name: "not a URL", endpoint: "engine-mcp:8085"},
		{name: "unsupported scheme", endpoint: "in-memory://test"},
		{name: "missing host", endpoint: "http:///mcp"},
		{name: "embedded credentials", endpoint: "http://user:password@engine-mcp:8085/mcp"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TEST_MCP_ENDPOINT", test.endpoint)
			_, err := agent.NewRuntime(agent.Config{
				Model:            "test-model",
				Deployment:       agent.NewTestDeployment(t),
				StructuredOutput: agent.StructuredOutputJSONSchema,
				AuditRecorder:    nopAuditRecorder{},
			})
			var domainErr *domain.Error
			if !errors.As(err, &domainErr) || domainErr.Code != domain.CodeMCPEndpointInvalid {
				t.Fatalf("NewRuntime() error = %v, want %s", err, domain.CodeMCPEndpointInvalid)
			}
		})
	}
}
