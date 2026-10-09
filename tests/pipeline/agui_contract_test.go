// These are static contract-regression tests: they assert pinned artifacts/tooling, not runtime behavior.
package pipeline_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

func validateContractFixture(t *testing.T, schemaPath, fixturePath string) {
	t.Helper()
	rawSchema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read AG-UI contract: %v", err)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		t.Fatalf("decode AG-UI contract: %v", err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve AG-UI contract: %v", err)
	}

	rawFixture, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read AG-UI fixture: %v", err)
	}
	var document any
	if err := json.Unmarshal(rawFixture, &document); err != nil {
		t.Fatalf("decode AG-UI fixture: %v", err)
	}
	if err := resolved.Validate(document); err != nil {
		t.Fatalf("validate AG-UI fixture: %v", err)
	}
}

func TestAGUIProductResultFixtureMatchesContract(t *testing.T) {
	validateContractFixture(
		t,
		"../../contracts/agui-product-profile.schema.json",
		"../../fixtures/agui-success-result.json",
	)
}

func TestAGUIForwardedPropsFixtureMatchesContract(t *testing.T) {
	validateContractFixture(
		t,
		"../../contracts/agui-forwarded-props.schema.json",
		"../../fixtures/agui-forwarded-props.json",
	)
}
