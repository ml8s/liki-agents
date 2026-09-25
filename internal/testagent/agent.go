// Package testagent provides a generic AgentDeployment for adapter tests.
// Domain behavior intentionally remains outside liki-agents.
package testagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ml8s/liki-agents/internal/agent"
)

func Deployment(t *testing.T) *agent.Deployment {
	t.Helper()
	t.Setenv("TEST_MCP_ENDPOINT", "in-memory://test")
	root := t.TempDir()
	files := map[string]string{
		"agent-deployment.json": `{
			"apiVersion": "agent.liki/v1",
			"kind": "AgentDeployment",
			"metadata": {"name": "adapter-agent", "version": "1.0.0"},
			"spec": {
				"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}],
				"agents": [{
					"name": "main",
					"version": "1.0.0",
					"description": "generic adapter test agent",
						"mode": "chat",
						"sub_agents": [],
					"instruction": {"path": "instruction.md"},
					"output": {
						"schema": {"path": "output.schema.json"},
						"textPointer": "/answer"
					},
					"tools": {"allow": {"test": ["test_tool"]}}
									}]
			}
		}`,
		"instruction.md": "You are a generic adapter test agent.",
		"output.schema.json": `{
			"$schema": "https://json-schema.org/draft/2020-12/schema",
			"type": "object",
			"additionalProperties": false,
			"required": ["answer"],
			"properties": {
				"answer": {"type": "string", "minLength": 1}
			}
		}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	deployment, err := agent.LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
	if err != nil {
		t.Fatalf("LoadAgentDeployment() error = %v", err)
	}
	return deployment
}
