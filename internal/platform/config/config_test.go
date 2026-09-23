package config_test

import (
	"strings"
	"testing"

	"github.com/liki/liki-agent/internal/platform/config"
)

func TestLoadVersionDefaults(t *testing.T) {
	t.Setenv("LIKI_ENGINE_MCP_URL", "http://127.0.0.1:18081/mcp")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PromptVersion != "chief-analysis-v1" {
		t.Fatalf("PromptVersion = %q", cfg.PromptVersion)
	}
	if cfg.PolicyVersion != "destiny-safety-v1" {
		t.Fatalf("PolicyVersion = %q", cfg.PolicyVersion)
	}
}

func TestLoadVersionOverrides(t *testing.T) {
	t.Setenv("LIKI_ENGINE_MCP_URL", "http://127.0.0.1:18081/mcp")
	t.Setenv("LIKI_PROMPT_VERSION", "chief-analysis-v2")
	t.Setenv("LIKI_POLICY_VERSION", "destiny-safety-v2")
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.PromptVersion != "chief-analysis-v2" || cfg.PolicyVersion != "destiny-safety-v2" {
		t.Fatalf("versions = %q/%q", cfg.PromptVersion, cfg.PolicyVersion)
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("LIKI_ENGINE_MCP_URL", "http://127.0.0.1:18081/mcp")
	t.Setenv("LIKI_RUN_TIMEOUT_SECONDS", "abc")
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid duration")
	}
	if !strings.Contains(err.Error(), "LIKI_RUN_TIMEOUT_SECONDS") {
		t.Fatalf("error = %v, want mention of LIKI_RUN_TIMEOUT_SECONDS", err)
	}
}

func TestLoadRejectsInvalidFloat(t *testing.T) {
	t.Setenv("LIKI_ENGINE_MCP_URL", "http://127.0.0.1:18081/mcp")
	t.Setenv("LIKI_LLM_TEMPERATURE", "hot")
	_, err := config.Load()
	if err == nil {
		t.Fatal("Load() expected error for invalid float")
	}
	if !strings.Contains(err.Error(), "LIKI_LLM_TEMPERATURE") {
		t.Fatalf("error = %v, want mention of LIKI_LLM_TEMPERATURE", err)
	}
}
