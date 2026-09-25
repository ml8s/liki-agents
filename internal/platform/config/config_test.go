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
	t.Setenv("LIKI_TOOL_CONTRACT_VERSION", "test-tool-contract")
	t.Setenv("LIKI_AGENTS_DEPLOYMENT_FILE", "/tmp/agent-deployment.json")
	t.Setenv("LIKI_LLM_PROVIDER", "zhipu")
	t.Setenv("LIKI_LLM_STRUCTURED_OUTPUT", "json_object")
	t.Setenv("LIKI_LLM_BASE_URL", "https://open.bigmodel.cn/api/v1")
}

func TestLoadValidConfiguration(t *testing.T) {
	baseEnv(t)
	t.Setenv("LIKI_LLM_STRUCTURED_OUTPUT", "none")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.LLMProvider != "zhipu" || cfg.LLMStructuredOutput != "" {
		t.Fatalf("provider/capability = %q/%q", cfg.LLMProvider, cfg.LLMStructuredOutput)
	}
	if cfg.DeploymentFile != "/tmp/agent-deployment.json" {
		t.Fatalf("definition file = %q", cfg.DeploymentFile)
	}
	if cfg.Topology != config.TopologySingle {
		t.Fatalf("topology = %q, want single", cfg.Topology)
	}
	t.Setenv("LIKI_MAX_CONCURRENT_RUNS", "7")
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load(max concurrent runs) error = %v", err)
	}
	if cfg.MaxConcurrentRuns != 7 {
		t.Fatalf("max concurrent runs = %d, want 7", cfg.MaxConcurrentRuns)
	}
	t.Setenv("LIKI_AGENTS_DEPLOYMENT_DIGEST", "sha256:"+strings.Repeat("a", 64))
	cfg, err = config.Load()
	if err != nil {
		t.Fatalf("Load(deployment digest) error = %v", err)
	}
	if cfg.DeploymentDigest != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("deployment digest = %q", cfg.DeploymentDigest)
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
		{name: "invalid deployment digest", key: "LIKI_AGENTS_DEPLOYMENT_DIGEST", value: "sha256:abc", wantErr: "LIKI_AGENTS_DEPLOYMENT_DIGEST"},
		{name: "multi topology is fail-closed", key: "LIKI_AGENTS_TOPOLOGY", value: "multi", wantErr: "LIKI_AGENTS_TOPOLOGY=multi is not supported yet"},
		{name: "invalid topology", key: "LIKI_AGENTS_TOPOLOGY", value: "cluster", wantErr: "LIKI_AGENTS_TOPOLOGY is unsupported"},
		{name: "unsupported provider", key: "LIKI_LLM_PROVIDER", value: "unknown", wantErr: "LIKI_LLM_PROVIDER is unsupported"},
		{name: "unsupported structured output", key: "LIKI_LLM_STRUCTURED_OUTPUT", value: "yaml", wantErr: "LIKI_LLM_STRUCTURED_OUTPUT is unsupported"},
		{name: "invalid public url", key: "LIKI_AGENTS_PUBLIC_URL", value: "agent.internal", wantErr: "LIKI_AGENTS_PUBLIC_URL must be an absolute HTTP(S) URL"},
		{name: "invalid llm url", key: "LIKI_LLM_BASE_URL", value: "api.example", wantErr: "LIKI_LLM_BASE_URL must be an absolute HTTP(S) URL"},
		{name: "invalid duration", key: "LIKI_RUN_TIMEOUT_SECONDS", value: "abc", wantErr: "LIKI_RUN_TIMEOUT_SECONDS"},
		{name: "zero MCP timeout", key: "LIKI_MCP_TIMEOUT_SECONDS", value: "0", wantErr: "LIKI_MCP_TIMEOUT_SECONDS"},
		{name: "zero concurrent runs", key: "LIKI_MAX_CONCURRENT_RUNS", value: "0", wantErr: "LIKI_MAX_CONCURRENT_RUNS"},
		{name: "invalid concurrent runs", key: "LIKI_MAX_CONCURRENT_RUNS", value: "many", wantErr: "LIKI_MAX_CONCURRENT_RUNS"},
		{name: "zero LLM timeout", key: "LIKI_LLM_TIMEOUT_SECONDS", value: "0", wantErr: "LIKI_LLM_TIMEOUT_SECONDS"},
		{name: "negative temperature", key: "LIKI_LLM_TEMPERATURE", value: "-0.1", wantErr: "LIKI_LLM_TEMPERATURE"},
		{name: "high temperature", key: "LIKI_LLM_TEMPERATURE", value: "2.1", wantErr: "LIKI_LLM_TEMPERATURE"},
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
