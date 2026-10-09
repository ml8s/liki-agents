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
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{
		"type": "object",
		"additionalProperties": false,
		"required": ["answer"],
		"properties": {"answer": {"type": "string"}}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0o600); err != nil {
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
			"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}],
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
				"skills": {"root": "/skills"},
				"tools": {"allow": {"test": ["test_tool"], "skilltoolset": ["list_skills", "load_skill", "load_skill_resource"]}}
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
	if len(deployment.Spec.MCPServers) != 1 || deployment.Spec.MCPServers[0].Name != "test" {
		t.Fatalf("MCP servers = %+v", deployment.Spec.MCPServers)
	}
	references := entrypoint.Tools.References()
	wantReferences := []ToolReference{
		{Server: "skilltoolset", Name: "list_skills"},
		{Server: "skilltoolset", Name: "load_skill"},
		{Server: "skilltoolset", Name: "load_skill_resource"},
		{Server: "test", Name: "test_tool"},
	}
	if len(references) != len(wantReferences) {
		t.Fatalf("tool references = %+v, want %+v", references, wantReferences)
	}
	for i, ref := range references {
		if ref != wantReferences[i] {
			t.Fatalf("tool references = %+v, want %+v", references, wantReferences)
		}
	}
}

func TestLoadAgentDeploymentRejectsInvalidManifests(t *testing.T) {
	testCases := []struct {
		name     string
		manifest string
		wantErr  string
	}{
		{name: "unknown field", manifest: `{"apiVersion":"agent.liki/v1","kind":"AgentDeployment","metadata":{"name":"x","version":"1"},"spec":{"agents":[{"name":"main","version":"1","description":"x","mode":"chat","instruction":{"path":"instruction.md"},"skills":{"root":"/skills"},"tools":{"allow":{"skilltoolset":["list_skills","load_skill","load_skill_resource"]}}}]},"extra":true}`, wantErr: "extra"},
		{name: "duplicate key", manifest: strings.Replace(validManifest(), `"kind": "AgentDeployment"`, `"kind": "AgentDeployment", "kind": "AgentDeployment"`, 1), wantErr: `duplicate key "kind" at $`},
		{name: "trailing value", manifest: validManifest() + ` {}`, wantErr: "trailing"},
		{name: "unsupported api", manifest: strings.Replace(validManifest(), `agent.liki/v1`, `agent.liki/v0`, 1), wantErr: `const: agent.liki/v0 does not equal agent.liki/v1`},
		{name: "unknown sub-agent", manifest: strings.Replace(validManifest(), `"sub_agents": []`, `"sub_agents": [{"name":"missing"}]`, 1), wantErr: "unknown sub-agent"},

		{name: "path traversal", manifest: strings.Replace(validManifest(), `instruction.md`, `../instruction.md`, 1), wantErr: "path escapes from parent"},
		{
			name: "unknown MCP server",
			manifest: strings.Replace(
				validManifest(),
				`"allow": {"test": ["test_tool"], "skilltoolset"`,
				`"allow": {"missing": ["test_tool"], "skilltoolset"`,
				1,
			),
			wantErr: `references unknown MCP server "missing"`,
		},
		{
			name: "duplicate MCP server",
			manifest: strings.Replace(
				validManifest(),
				`"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}]`,
				`"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}, {"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}]`,
				1,
			),
			wantErr: `duplicate MCP server "test"`,
		},
		{
			name: "duplicate tool name across servers",
			manifest: strings.Replace(
				strings.Replace(
					validManifest(),
					`"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}]`,
					`"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}, {"name": "other", "endpointEnv": "OTHER_MCP_ENDPOINT"}]`,
					1,
				),
				`"allow": {"test": ["test_tool"], "skilltoolset"`,
				`"allow": {"test": ["test_tool"], "other": ["test_tool"], "skilltoolset"`,
				1,
			),
			wantErr: `allowlisted by multiple MCP servers`,
		},
		{
			name: "skills root must match absolute path pattern",
			manifest: strings.Replace(validManifest(), `"skills": {"root": "/skills"}`,
				`"skills": {"root": "skills"}`, 1),
			wantErr: "pattern",
		},
		{
			name: "skills root must not be empty",
			manifest: strings.Replace(validManifest(), `"skills": {"root": "/skills"}`,
				`"skills": {"root": ""}`, 1),
			wantErr: "minLength",
		},
		{
			name: "skills preload must be enum",
			manifest: strings.Replace(validManifest(), `"skills": {"root": "/skills"}`,
				`"skills": {"root": "/skills", "preload": "lazy"}`, 1),
			wantErr: "enum",
		},
		{
			name: "skills binding requires allow triple",
			manifest: strings.Replace(validManifest(),
				`, "skilltoolset": ["list_skills", "load_skill", "load_skill_resource"]`, ``, 1),
			wantErr: "skills binding requires tools.allow[skilltoolset]",
		},
		{
			name: "allow triple requires skills binding",
			manifest: strings.Replace(validManifest(),
				`"skills": {"root": "/skills"},
				"tools"`, `"tools"`, 1),
			wantErr: "tools.allow[skilltoolset] requires a skills binding",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(testCase.manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("LoadAgentDeployment() error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestLoadAgentDeploymentRejectsAmbiguousOutputSchema(t *testing.T) {
	manifest := strings.Replace(validManifest(), `"answer": {"type": "string"}`, `"answer": {"type": "string"}, "answer": {"type": "string"}`, 1)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object","answer":{"type":"string"},"answer":{"type":"string"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
	if err == nil || !strings.Contains(err.Error(), `duplicate key "answer"`) {
		t.Fatalf("LoadAgentDeployment() error = %v, want duplicate output schema key rejection", err)
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

func TestAgentOutputPointerMustSelectAStringAtLoadTime(t *testing.T) {
	tests := []struct {
		name    string
		pointer string
		schema  string
		wantErr string
	}{
		{
			name:    "missing property",
			pointer: "/missing",
			schema:  `{"type":"object","properties":{"answer":{"type":"string"}}}`,
			wantErr: `token "missing" has no statically known property schema`,
		},
		{
			name:    "number property",
			pointer: "/answer",
			schema:  `{"type":"object","properties":{"answer":{"type":"number"}}}`,
			wantErr: "must select a string schema",
		},
		{
			name:    "array item",
			pointer: "/answers/0",
			schema:  `{"type":"object","properties":{"answers":{"type":"array","items":{"type":"string"}}}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			manifest := strings.Replace(validManifest(), `"/answer"`, `"`+test.pointer+`"`, 1)
			if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(test.schema), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json"))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("LoadAgentDeployment() error = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("LoadAgentDeployment() error = %v, want %q", err, test.wantErr)
			}
		})
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
				"skills": {"root": "/skills"},
				"tools": {"allow": {"skilltoolset": ["list_skills", "load_skill", "load_skill_resource"]}}
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
		if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0o600); err != nil {
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
			if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic instruction"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(manifest), 0o600); err != nil {
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
	worker := `{"name":"worker","version":"1.0.0","description":"worker agent","mode":"task","sub_agents":[],"instruction":{"path":"instruction.md"},"output":{"schema":{"path":"output.schema.json"},"textPointer":"/answer"},"skills":{"root":"/skills"},"tools":{"allow":{"test":["test_tool"],"skilltoolset":["list_skills","load_skill","load_skill_resource"]}}}`
	base := `{
		"apiVersion": "agent.liki/v1",
		"kind": "AgentDeployment",
		"metadata": {"name": "generic-agents", "version": "1.0.0"},
		"spec": {
			"mcpServers": [{"name": "test", "endpointEnv": "TEST_MCP_ENDPOINT"}],
			"agents": [
				{"name": "coordinator", "version": "1.0.0", "description": "coordinator", "mode": "chat", "sub_agents": [{"name": "worker"}], "instruction": {"path": "instruction.md"}, "output": {"schema": {"path": "output.schema.json"}, "textPointer": "/answer"}, "skills": {"root": "/skills"}, "tools": {"allow": {"test": ["test_tool"], "skilltoolset": ["list_skills", "load_skill", "load_skill_resource"]}}},
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
	if err := os.WriteFile(filepath.Join(root, "instruction.md"), []byte("generic"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "output.schema.json"), []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "agent-deployment.json"), []byte(cycle), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAgentDeployment(filepath.Join(root, "agent-deployment.json")); err == nil || !strings.Contains(err.Error(), "exactly one root Agent") {
		t.Fatalf("LoadAgentDeployment() error = %v, want cyclic graph rejection", err)
	}
}
