// Package contracts embeds the versioned external artifact schemas.
package contracts

import (
	_ "embed"
	"encoding/json"
	"sync"

	"github.com/google/jsonschema-go/jsonschema"
)

//go:embed agent-definition.schema.json
var agentDefinitionSchema []byte

var (
	agentDefinitionOnce sync.Once
	agentDefinition     *jsonschema.Resolved
	agentDefinitionErr  error
)

// AgentDefinition resolves the canonical JSON Schema for AgentDeployment
// artifacts. The returned value is read-only and safe for concurrent reuse.
func AgentDefinition() (*jsonschema.Resolved, error) {
	agentDefinitionOnce.Do(func() {
		var schema jsonschema.Schema
		if err := json.Unmarshal(agentDefinitionSchema, &schema); err != nil {
			agentDefinitionErr = err
			return
		}
		agentDefinition, agentDefinitionErr = schema.Resolve(nil)
	})
	return agentDefinition, agentDefinitionErr
}
