package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDeployment(t *testing.T, root string, manifest string) *Deployment {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["answer"],
		"properties": {"answer": {"type": "string"}}
	}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	deployment, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
	if err != nil {
		t.Fatalf("LoadAgentDeployment() error = %v", err)
	}
	return deployment
}

func validManifest() string {
	return `{
		"apiVersion": "agent.liki/v1",
		"kind": "AgentDeployment",
		"metadata": {"name": "generic-agents", "version": "1.0.0"},
		"spec": {
			"agents": [{
				"name": "main",
				"version": "1.0.0",
				"description": "generic main agent",
				"mode": "chat",
				"sub_agents": [],
				"instruction": {"path": "instruction.md"},
				"output": {
					"schema": {"path": "output.schema.json"},
					"textPointer": "/answer"
				},
				"tools": {"allow": ["test_tool"]}
			}]
		}
	}`
}

func TestLoadAgentDeployment(t *testing.T) {
	root := t.TempDir()
	deployment := writeDeployment(t, root, validManifest())
	if deployment.Metadata.Name != "generic-agents" || deployment.Metadata.Version != "1.0.0" {
		t.Fatalf("metadata = %+v", deployment.Metadata)
	}
	if deployment.Digest == "" || !strings.HasPrefix(deployment.Digest, "sha256:") {
		t.Fatalf("digest = %q", deployment.Digest)
	}
	entrypoint, err := deployment.EntrypointDefinition()
	if err != nil {
		t.Fatalf("EntrypointDefinition() error = %v", err)
	}
	if entrypoint.InstructionText != "generic instruction" {
		t.Fatalf("instruction = %q", entrypoint.InstructionText)
	}
	if entrypoint.InstructionDigest == "" || entrypoint.SchemaDigest == "" {
		t.Fatalf("artifact digests = %q/%q", entrypoint.InstructionDigest, entrypoint.SchemaDigest)
	}
	if len(entrypoint.GenaiOutputSchema.Properties) != 1 {
		t.Fatalf("output properties = %+v", entrypoint.GenaiOutputSchema.Properties)
	}
}

func TestLoadAgentDeploymentRejectsInvalidManifests(t *testing.T) {
	testCases := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{name: "unknown field", manifest: `{"apiVersion":"agent.liki/v1","kind":"AgentDeployment","metadata":{"name":"x","version":"1"},"spec":{"agents":[{"name":"main","version":"1","description":"x","mode":"chat","instruction":{"path":"instruction.md"},"tools":{"allow":[]}}]},"extra":true}`, wantErr: "extra"},
		{name: "unsupported api", manifest: strings.Replace(validManifest(), `agent.liki/v1`, `agent.liki/v0`, 1), wantErr: `const: agent.liki/v0 does not equal agent.liki/v1`},
		{name: "unknown sub-agent", manifest: strings.Replace(validManifest(), `"sub_agents": []`, `"sub_agents": [{"name":"missing"}]`, 1), wantErr: "unknown sub-agent"},

		{name: "path traversal", manifest: strings.Replace(validManifest(), `instruction.md`, `../instruction.md`, 1), wantErr: "path escapes from parent"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(testCase.manifest), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("LoadAgentDeployment() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestAgentDefinitionOutputIsStrictlyValidated(t *testing.T) {
	root := t.TempDir()
	deployment := writeDeployment(t, root, validManifest())
	entrypoint, err := deployment.EntrypointDefinition()
	if err != nil {
		t.Fatal(err)
	}
	if err := entrypoint.ResolvedOutput.Validate(map[string]any{"answer": "ok"}); err != nil {
		t.Fatalf("Validate(valid output) error = %v", err)
	}
	err = entrypoint.ResolvedOutput.Validate(map[string]any{"answer": "ok", "extra": true})
	if err == nil {
		t.Fatal("Validate(extra output) unexpectedly succeeded")
	}
}

func TestAgentOutputIsOptional(t *testing.T) {
	plainManifest := `{
		"apiVersion": "agent.liki/v1",
		"kind": "AgentDeployment",
		"metadata": {"name": "generic-agents", "version": "1.0.0"},
		"spec": {
			"agents": [{
				"name": "main",
				"version": "1.0.0",
				"description": "generic main agent",
				"mode": "chat",
				"instruction": {"path": "instruction.md"},
				"tools": {"allow": []}
			}]
		}
	}`
	deployment := writeDeployment(t, t.TempDir(), plainManifest)
	entrypoint, err := deployment.EntrypointDefinition()
	if err != nil {
		t.Fatal(err)
	}
	if entrypoint.Output.Structured() {
		t.Fatal("agent output was structured without an output declaration")
	}
	if entrypoint.OutputSchema != nil || entrypoint.ResolvedOutput != nil || entrypoint.GenaiOutputSchema != nil {
		t.Fatal("plain agent resolved an output schema")
	}
	if entrypoint.SchemaDigest != "" {
		t.Fatalf("plain agent schema digest = %q, want empty", entrypoint.SchemaDigest)
	}
}

func TestAgentOutputRequiresPairedSchemaAndPointer(t *testing.T) {
	t.Run("root pointer", func(t *testing.T) {
		manifest := strings.Replace(validManifest(), `"/answer"`, `"/"`, 1)
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
		if err == nil || !strings.Contains(err.Error(), `"/" does not match regular expression "^/(?:~[01]|[^~])+(?:/(?:~[01]|[^~])+)*$"`) {
			t.Fatalf("LoadAgentDeployment() error = %v, want JSON Schema root-pointer rejection", err)
		}
	})

	testCases := []struct {
		name    string
		wantErr string
	}{
		{
			name:    "schema without pointer",
			wantErr: `dependentRequired["schema"]`,
		},
		{
			name:    "pointer without schema",
			wantErr: `dependentRequired["textPointer"]`,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			manifest := `{
				"apiVersion": "agent.liki/v1",
				"kind": "AgentDeployment",
				"metadata": {"name": "generic-agents", "version": "1.0.0"},
				"spec": {
					"agents": [{
						"name": "main",
						"version": "1.0.0",
						"description": "generic main agent",
						"mode": "chat",
						"instruction": {"path": "instruction.md"},
						"output": {"schema": {"path": "output.schema.json"}}
					}]
				}
			}`
			switch testCase.name {
			case "pointer without schema":
				manifest = strings.Replace(manifest, `"schema": {"path": "output.schema.json"}`, `"textPointer": "/answer"`, 1)
			}
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("LoadAgentDeployment() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestLoadAgentDeploymentValidatesGraph(t *testing.T) {
	worker := `{"name":"worker","version":"1.0.0","description":"worker agent","mode":"task","sub_agents":[],"instruction":{"path":"instruction.md"},"output":{"schema":{"path":"output.schema.json"},"textPointer":"/answer"},"tools":{"allow":["test_tool"]}}`
	base := `{
		"apiVersion": "agent.liki/v1",
		"kind": "AgentDeployment",
		"metadata": {"name": "generic-agents", "version": "1.0.0"},
		"spec": {
			"agents": [
				{"name": "coordinator", "version": "1.0.0", "description": "coordinator", "mode": "chat", "sub_agents": [{"name": "worker"}], "instruction": {"path": "instruction.md"}, "output": {"schema": {"path": "output.schema.json"}, "textPointer": "/answer"}, "tools": {"allow": ["test_tool"]}},
				%s
			]
		}
	}`
	manifest := fmt.Sprintf(base, worker)

	root := t.TempDir()
	deployment := writeDeployment(t, root, manifest)
	if len(deployment.Spec.Agents) != 2 {
		t.Fatalf("agents = %d, want 2", len(deployment.Spec.Agents))
	}
	if deployment.Spec.Agents[1].Name != "worker" {
		t.Fatalf("worker = %+v", deployment.Spec.Agents[1])
	}

	cycle := strings.Replace(manifest, `"sub_agents":[]`, `"sub_agents":[{"name":"coordinator"}]`, 1)
	if cycle == manifest {
		cycle = strings.Replace(manifest, `"sub_agents": []`, `"sub_agents": [{"name":"coordinator"}]`, 1)
	}
	root = t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(cycle), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json")); err == nil || !strings.Contains(err.Error(), "exactly one root Agent") {
		t.Fatalf("LoadAgentDeployment() error = %v, want cyclic graph rejection", err)
	}
}
