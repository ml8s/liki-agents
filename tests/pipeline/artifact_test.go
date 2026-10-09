// These are static contract-regression tests: they assert pinned artifacts/tooling, not runtime behavior.
package pipeline_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ml8s/liki-agents/contracts"
	agentruntime "github.com/ml8s/liki-agents/internal/agent"
)

func TestDevelopmentAgentArtifactIsLoadable(t *testing.T) {
	if _, err := agentruntime.LoadAgentDeployment("../../dev/agent-deployment/deployment.json"); err != nil {
		t.Fatalf("LoadAgentDeployment() error = %v", err)
	}
}

func TestAgentDefinitionContractIsValidJSON(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/agent-definition.schema.json")
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode contract: %v", err)
	}
	if document["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("contract dialect = %#v, want JSON Schema 2020-12", document["$schema"])
	}
}

func TestAgentDefinitionContractResolvesAndValidatesDevelopmentArtifact(t *testing.T) {
	contract, err := contracts.AgentDefinition()
	if err != nil {
		t.Fatalf("resolve contract: %v", err)
	}
	raw, err := os.ReadFile("../../dev/agent-deployment/deployment.json")
	if err != nil {
		t.Fatalf("read development artifact: %v", err)
	}
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatalf("decode development artifact: %v", err)
	}
	if err := contract.Validate(document); err != nil {
		t.Fatalf("development artifact fails contract: %v", err)
	}
}
