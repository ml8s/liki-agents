package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func canonicalDeploymentPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	path := filepath.Join(filepath.Dir(file), "..", "dev", "agent-deployment", "deployment.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("canonical deployment fixture: %v", err)
	}
	return path
}

func decodeFile(t *testing.T, path string) any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestAgentDefinitionResolves(t *testing.T) {
	schema, err := AgentDefinition()
	if err != nil {
		t.Fatalf("AgentDefinition() error = %v", err)
	}
	if schema == nil {
		t.Fatal("AgentDefinition() returned nil schema")
	}
	if base := schema.Schema(); base == nil {
		t.Fatal("Resolved.Schema() returned nil")
	}
}

func TestAgentDefinitionValidatesCanonicalDeployment(t *testing.T) {
	schema, err := AgentDefinition()
	if err != nil {
		t.Fatalf("AgentDefinition() error = %v", err)
	}
	if err := schema.Validate(decodeFile(t, canonicalDeploymentPath(t))); err != nil {
		t.Fatalf("canonical deployment rejected: %v", err)
	}
}

func TestAgentDefinitionRemainsCompatibleWithV1MinimalDeployment(t *testing.T) {
	schema, err := AgentDefinition()
	if err != nil {
		t.Fatalf("AgentDefinition() error = %v", err)
	}
	document := map[string]any{
		"apiVersion": "agent.liki/v1",
		"kind":       "AgentDeployment",
		"metadata":   map[string]any{"name": "compatibility", "version": "1.0.0"},
		"spec": map[string]any{
			"agents": []any{
				map[string]any{
					"name":        "main",
					"version":     "1.0.0",
					"description": "v1 compatibility fixture",
					"mode":        "chat",
					"instruction": map[string]any{"path": "instruction.md"},
					"tools":       map[string]any{"allow": map[string]any{}},
				},
			},
		},
	}
	if err := schema.Validate(document); err != nil {
		t.Fatalf("v1 minimal deployment rejected: %v", err)
	}
}

func TestAgentDefinitionRejectsIncompleteDocuments(t *testing.T) {
	schema, err := AgentDefinition()
	if err != nil {
		t.Fatalf("AgentDefinition() error = %v", err)
	}
	testCases := map[string]string{
		"empty object":       `{}`,
		"missing spec":       `{"apiVersion":"agent.liki/v1","kind":"AgentDeployment","metadata":{"name":"x","version":"1.0.0"}}`,
		"wrong kind":         `{"apiVersion":"agent.liki/v1","kind":"Deployment","metadata":{"name":"x","version":"1.0.0"},"spec":{}}`,
		"missing version":    `{"apiVersion":"agent.liki/v1","kind":"AgentDeployment","metadata":{"name":"x"},"spec":{}}`,
		"unknown apiVersion": `{"apiVersion":"agent.liki/v2","kind":"AgentDeployment","metadata":{"name":"x","version":"1.0.0"},"spec":{}}`,
	}
	for name, raw := range testCases {
		var document any
		if err := json.Unmarshal([]byte(raw), &document); err != nil {
			t.Fatalf("%s: invalid test JSON: %v", name, err)
		}
		if err := schema.Validate(document); err == nil {
			t.Errorf("%s: expected validation error, got nil", name)
		}
	}
}

func TestAgentDefinitionReturnsStableSchema(t *testing.T) {
	first, err := AgentDefinition()
	if err != nil {
		t.Fatalf("AgentDefinition() error = %v", err)
	}
	const readers = 8
	var wg sync.WaitGroup
	results := make([]*jsonschema.Resolved, readers)
	errs := make([]error, readers)
	wg.Add(readers)
	for i := 0; i < readers; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = AgentDefinition()
		}(i)
	}
	wg.Wait()
	for i := 0; i < readers; i++ {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
		if results[i] != first {
			t.Fatalf("reader %d: resolved schema pointer drifted (sync.Once contract)", i)
		}
	}
}
