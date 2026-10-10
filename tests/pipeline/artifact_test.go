// These are static contract-regression tests: they assert pinned artifacts/tooling, not runtime behavior.
package pipeline_test

import (
	"encoding/json"
	"os"
	"testing"
)

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
