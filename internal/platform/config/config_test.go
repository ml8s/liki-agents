package config_test

import (
	"strings"
	"testing"

	"github.com/ml8s/liki-agents/internal/platform/config"
)

func baseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("LIKI_ENV", "development")
	t.Setenv("LIKI_AGENTS_PUBLIC_URL", "https://agent.internal")
	t.Setenv("LIKI_ENGINE_MCP_URL", "http://127.0.0.1:18081/mcp")
	t.Setenv("LIKI_ENGINE_CONTRACT_VERSION", "test-engine")
	t.Setenv("LIKI_AGENTS_DEPLOYMENT_FILE", "/tmp/agent-deployment.json")
	t.Setenv("LIKI_LLM_PROVIDER", "zhipu")
	t.Setenv("LIKI_LLM_STRUCTURED_OUTPUT", "json_object")
	t.Setenv("LIKI_LLM_BASE_URL", "https://open.bigmodel.cn/api/v1")
}

func TestLoadValidConfiguration(t *testing.T) {
	baseEnv(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMProvider != "zhipu" || cfg.LLMStructuredOutput != "json_object" {
		t.Fatalf("provider/capability = %q/%q", cfg.LLMProvider, cfg.LLMStructuredOutput)
	}
	if cfg.DeploymentFile != "/tmp/agent-deployment.json" {
		t.Fatalf("definition file = %q", cfg.DeploymentFile)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	testCases := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{name: "missing deployment", key: "LIKI_AGENTS_DEPLOYMENT_FILE", value: "", wantErr: "LIKI_AGENTS_DEPLOYMENT_FILE is required"},
		{name: "unsupported provider", key: "LIKI_LLM_PROVIDER", value: "unknown", wantErr: "LIKI_LLM_PROVIDER is unsupported"},
		{name: "unsupported structured output", key: "LIKI_LLM_STRUCTURED_OUTPUT", value: "yaml", wantErr: "LIKI_LLM_STRUCTURED_OUTPUT is unsupported"},
		{name: "invalid public url", key: "LIKI_AGENTS_PUBLIC_URL", value: "agent.internal", wantErr: "LIKI_AGENTS_PUBLIC_URL must be an absolute HTTP(S) URL"},
		{name: "invalid llm url", key: "LIKI_LLM_BASE_URL", value: "api.example", wantErr: "LIKI_LLM_BASE_URL must be an absolute HTTP(S) URL"},
		{name: "invalid duration", key: "LIKI_RUN_TIMEOUT_SECONDS", value: "abc", wantErr: "LIKI_RUN_TIMEOUT_SECONDS"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			baseEnv(t)
			t.Setenv(testCase.key, testCase.value)
			_, err := config.Load()
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("Load() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}
