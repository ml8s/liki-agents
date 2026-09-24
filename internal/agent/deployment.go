// Package agent: external AgentDeployment artifacts are the only attachment
// point for domain prompt, output schema, tool allowlist, and Agent identity.
package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/ml8s/liki-agents/contracts"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/genai"
)

const (
	AgentAPIVersion = "agent.liki/v1"
	AgentKind       = "AgentDeployment"
)

type Deployment struct {
	APIVersion string             `json:"apiVersion"`
	Kind       string             `json:"kind"`
	Metadata   DeploymentMetadata `json:"metadata"`
	Spec       DeploymentSpec     `json:"spec"`

	Digest string `json:"-"`
}

type DeploymentMetadata struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type DeploymentSpec struct {
	Agents []AgentDefinition `json:"agents"`
}

type AgentMode string

const (
	AgentModeChat       AgentMode = "chat"
	AgentModeTask       AgentMode = "task"
	AgentModeSingleTurn AgentMode = "single_turn"
)

type AgentReference struct {
	Name string `json:"name"`
}

type AgentDefinition struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Mode        AgentMode        `json:"mode"`
	SubAgents   []AgentReference `json:"sub_agents"`
	Instruction FileReference    `json:"instruction"`
	Output      OutputDefinition `json:"output"`
	Tools       ToolAllowlist    `json:"tools"`

	// Runtime-only resolved values. They are never read from JSON.
	InstructionDigest string               `json:"-"`
	InstructionText   string               `json:"-"`
	SchemaDigest      string               `json:"-"`
	OutputSchema      *jsonschema.Schema   `json:"-"`
	ResolvedOutput    *jsonschema.Resolved `json:"-"`
	GenaiOutputSchema *genai.Schema        `json:"-"`
	RawOutputSchema   map[string]any       `json:"-"`
	Digest            string               `json:"-"`
}

type FileReference struct {
	Path string `json:"path"`
}

type OutputDefinition struct {
	Schema      FileReference `json:"schema"`
	TextPointer string        `json:"textPointer"`
}

func (o OutputDefinition) Structured() bool {
	return o.Schema.Path != "" || o.TextPointer != ""
}

type ToolAllowlist struct {
	Allow []string `json:"allow"`
}

type DefinitionRef struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type definitionLoader struct {
	root string
}

func LoadAgentDeployment(path string) (*Deployment, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("agent deployment file is required")
	}
	root, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, fmt.Errorf("resolve agent deployment directory: %w", err)
	}
	loader := &definitionLoader{root: root}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent deployment: %w", err)
	}
	if err := validateAgainstContract(raw); err != nil {
		return nil, fmt.Errorf("validate agent deployment contract: %w", err)
	}
	deployment, err := decodeAgentDeployment(raw)
	if err != nil {
		return nil, err
	}
	if err := deployment.validate(); err != nil {
		return nil, err
	}
	if err := loader.loadAgents(deployment); err != nil {
		return nil, err
	}
	if deployment.Digest, err = deploymentDigest(raw, deployment); err != nil {
		return nil, fmt.Errorf("deployment digest: %w", err)
	}
	return deployment, nil
}

func validateAgainstContract(raw []byte) error {
	contract, err := contracts.AgentDefinition()
	if err != nil {
		return fmt.Errorf("resolve JSON Schema contract: %w", err)
	}
	var document any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&document); err != nil {
		return fmt.Errorf("decode JSON document: %w", err)
	}
	if err := contract.Validate(document); err != nil {
		return fmt.Errorf("JSON Schema validation failed: %w", err)
	}
	return nil
}

// Validate rechecks an in-memory deployment, including its derived graph
// invariants. This is useful after a caller composes a test or migration
// artifact without changing immutable files.
func (d *Deployment) Validate() error {
	return d.validate()
}

func decodeAgentDeployment(raw []byte) (*Deployment, error) {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var deployment Deployment
	if err := decoder.Decode(&deployment); err != nil {
		return nil, fmt.Errorf("decode agent deployment: %w", err)
	}
	return &deployment, nil
}

func (d *Deployment) validate() error {
	if d.APIVersion != AgentAPIVersion {
		return fmt.Errorf("unsupported agent deployment apiVersion %q", d.APIVersion)
	}
	if d.Kind != AgentKind {
		return fmt.Errorf("unsupported agent deployment kind %q", d.Kind)
	}
	if strings.TrimSpace(d.Metadata.Name) == "" {
		return fmt.Errorf("agent deployment metadata.name is required")
	}
	if strings.TrimSpace(d.Metadata.Version) == "" {
		return fmt.Errorf("agent deployment metadata.version is required")
	}
	if len(d.Spec.Agents) == 0 {
		return fmt.Errorf("agent deployment must contain at least one agent")
	}

	names := make(map[string]struct{}, len(d.Spec.Agents))
	for index := range d.Spec.Agents {
		agent := &d.Spec.Agents[index]
		if err := agent.validate(); err != nil {
			return fmt.Errorf("agent[%d]: %w", index, err)
		}
		if _, exists := names[agent.Name]; exists {
			return fmt.Errorf("duplicate agent name %q", agent.Name)
		}
		names[agent.Name] = struct{}{}
		names[agent.Name] = struct{}{}
	}
	return d.validateAgentGraph(names)
}

func (d *Deployment) validateAgentGraph(names map[string]struct{}) error {
	callers := make(map[string]string, len(d.Spec.Agents))
	incoming := make(map[string]int, len(d.Spec.Agents))
	for name := range names {
		incoming[name] = 0
	}
	for _, agent := range d.Spec.Agents {
		seen := make(map[string]struct{}, len(agent.SubAgents))
		for index, reference := range agent.SubAgents {
			target := strings.TrimSpace(reference.Name)
			if target == "" {
				return fmt.Errorf("agent %q sub_agents[%d]: name is required", agent.Name, index)
			}
			if _, exists := names[target]; !exists {
				return fmt.Errorf("agent %q references unknown sub-agent %q", agent.Name, target)
			}
			if target == agent.Name {
				return fmt.Errorf("agent %q cannot be its own sub-agent", agent.Name)
			}
			if _, exists := seen[target]; exists {
				return fmt.Errorf("agent %q has duplicate sub-agent %q", agent.Name, target)
			}
			if previous, exists := callers[target]; exists {
				return fmt.Errorf("agent %q has multiple parents (%q and %q); AgentDeployments must form a tree", target, previous, agent.Name)
			}
			callers[target] = agent.Name
			incoming[target]++
			seen[target] = struct{}{}
		}
	}

	roots := make([]string, 0, 1)
	for name, count := range incoming {
		if count == 0 {
			roots = append(roots, name)
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("deployment must have exactly one root Agent, got %d", len(roots))
	}
	root := roots[0]

	visiting := make(map[string]struct{})
	visited := make(map[string]struct{})
	definitions := make(map[string]*AgentDefinition, len(d.Spec.Agents))
	for index := range d.Spec.Agents {
		definitions[d.Spec.Agents[index].Name] = &d.Spec.Agents[index]
	}

	var visit func(string) error
	visit = func(name string) error {
		switch _, done := visited[name]; {
		case done:
			return nil
		case visiting[name] != struct{}{}:
			return fmt.Errorf("agent delegation graph contains a cycle involving %q", name)
		}
		visiting[name] = struct{}{}
		for _, reference := range definitions[name].SubAgents {
			if err := visit(reference.Name); err != nil {
				return err
			}
		}
		delete(visiting, name)
		visited[name] = struct{}{}
		return nil
	}
	for _, agent := range d.Spec.Agents {
		if err := visit(agent.Name); err != nil {
			return err
		}
	}

	reachable := make(map[string]struct{})
	var walk func(string)
	walk = func(name string) {
		if _, done := reachable[name]; done {
			return
		}
		reachable[name] = struct{}{}
		for _, reference := range definitions[name].SubAgents {
			walk(reference.Name)
		}
	}
	walk(root)
	for name := range names {
		if _, ok := reachable[name]; !ok {
			return fmt.Errorf("agent %q is not reachable from root %q", name, root)
		}
	}
	return nil
}

func (a *AgentDefinition) validate() error {
	if strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(a.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("description is required")
	}
	if strings.TrimSpace(a.Instruction.Path) == "" {
		return fmt.Errorf("instruction.path is required")
	}
	if a.Output.Structured() {
		if strings.TrimSpace(a.Output.Schema.Path) == "" {
			return fmt.Errorf("output.schema.path is required when structured output is enabled")
		}
		if a.Output.TextPointer == "/" {
			return fmt.Errorf("output.textPointer cannot select the object root")
		}
		if !strings.HasPrefix(a.Output.TextPointer, "/") {
			return fmt.Errorf("output.textPointer must be a JSON Pointer when structured output is enabled")
		}
	}
	switch a.Mode {
	case AgentModeChat, AgentModeTask, AgentModeSingleTurn:
	default:
		return fmt.Errorf("unsupported mode %q", a.Mode)
	}
	if a.Name == "user" {
		return fmt.Errorf(`name %q is reserved by ADK`, a.Name)
	}
	if strings.Contains(a.Name, ".") || strings.ContainsAny(a.Name, " \t\r\n") {
		return fmt.Errorf("name %q contains characters incompatible with ADK branch paths", a.Name)
	}
	seen := make(map[string]struct{}, len(a.Tools.Allow))
	for _, tool := range a.Tools.Allow {
		if tool == "" || strings.TrimSpace(tool) != tool {
			return fmt.Errorf("tools.allow contains an empty tool name")
		}
		if len(tool) > 128 {
			return fmt.Errorf("tools.allow tool name exceeds MCP maximum length of 128")
		}
		for _, char := range tool {
			if char >= 'a' && char <= 'z' ||
				char >= 'A' && char <= 'Z' ||
				char >= '0' && char <= '9' ||
				char == '_' || char == '-' || char == '.' {
				continue
			}
			return fmt.Errorf("tools.allow tool name %q contains a character invalid for MCP", tool)
		}
		if _, exists := seen[tool]; exists {
			return fmt.Errorf("duplicate tool %q", tool)
		}
		seen[tool] = struct{}{}
	}
	return nil
}

func (l *definitionLoader) loadAgents(deployment *Deployment) error {
	for index := range deployment.Spec.Agents {
		agent := &deployment.Spec.Agents[index]
		instructionRaw, err := l.readFile(agent.Instruction.Path)
		if err != nil {
			return fmt.Errorf("agent %q instruction: %w", agent.Name, err)
		}
		instruction := string(instructionRaw)
		if strings.TrimSpace(instruction) == "" {
			return fmt.Errorf("agent %q instruction is empty", agent.Name)
		}
		agent.InstructionText = instruction
		agent.InstructionDigest = sha256Hex([]byte(instruction))
		if agent.Output.Structured() {
			schemaRaw, err := l.readFile(agent.Output.Schema.Path)
			if err != nil {
				return fmt.Errorf("agent %q output schema: %w", agent.Name, err)
			}
			var outputSchema jsonschema.Schema
			decoder := json.NewDecoder(strings.NewReader(string(schemaRaw)))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&outputSchema); err != nil {
				return fmt.Errorf("agent %q decode output schema: %w", agent.Name, err)
			}
			resolved, err := outputSchema.Resolve(nil)
			if err != nil {
				return fmt.Errorf("agent %q resolve output schema: %w", agent.Name, err)
			}
			genaiSchema, err := convertJSONSchemaToGenai(&outputSchema)
			if err != nil {
				return fmt.Errorf("agent %q output schema: %w", agent.Name, err)
			}
			if genaiSchema.Type != genai.TypeObject {
				return fmt.Errorf("agent %q output schema root must be an object", agent.Name)
			}
			agent.OutputSchema = &outputSchema
			agent.ResolvedOutput = resolved
			agent.SchemaDigest = sha256Hex(schemaRaw)
			agent.GenaiOutputSchema = genaiSchema
			var rawOutputSchema map[string]any
			rawDecoder := json.NewDecoder(strings.NewReader(string(schemaRaw)))
			rawDecoder.UseNumber()
			if err := rawDecoder.Decode(&rawOutputSchema); err != nil {
				return fmt.Errorf("agent %q decode raw output schema: %w", agent.Name, err)
			}
			agent.RawOutputSchema = rawOutputSchema
		}
		agent.Digest, err = agentDigest(*agent)
		if err != nil {
			return fmt.Errorf("agent %q digest: %w", agent.Name, err)
		}
	}
	return nil
}

func (l *definitionLoader) readFile(path string) ([]byte, error) {
	root, err := os.OpenRoot(l.root)
	if err != nil {
		return nil, fmt.Errorf("open agent deployment directory: %w", err)
	}
	defer root.Close()
	data, err := root.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func convertJSONSchemaToGenai(schema *jsonschema.Schema) (*genai.Schema, error) {
	if schema == nil {
		return nil, errors.New("schema is required")
	}
	if schema.Ref != "" || len(schema.AllOf) != 0 || len(schema.OneOf) != 0 || len(schema.AnyOf) != 0 || schema.Not != nil {
		return nil, errors.New("unsupported schema composition")
	}
	if len(schema.Types) != 0 {
		return nil, errors.New("schema type arrays are unsupported")
	}
	result := &genai.Schema{
		Description: schema.Description,
		Enum:        make([]string, 0, len(schema.Enum)),
		Format:      schema.Format,
		Title:       schema.Title,
	}
	for _, value := range schema.Enum {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, fmt.Errorf("encode enum value: %w", err)
		}
		var text string
		if err := json.Unmarshal(encoded, &text); err != nil {
			return nil, errors.New("only string enums are supported")
		}
		result.Enum = append(result.Enum, text)
	}
	if schema.Type != "" {
		genaiType, err := genaiType(schema.Type)
		if err != nil {
			return nil, err
		}
		result.Type = genaiType
	}
	copyInt64 := func(value *int) (*int64, error) {
		if value == nil {
			return nil, nil
		}
		converted := int64(*value)
		return &converted, nil
	}
	copyFloat64 := func(value *float64) (*float64, error) {
		if value == nil {
			return nil, nil
		}
		converted := *value
		return &converted, nil
	}
	var err error
	if result.MinItems, err = copyInt64(schema.MinItems); err != nil {
		return nil, err
	}
	if result.MaxItems, err = copyInt64(schema.MaxItems); err != nil {
		return nil, err
	}
	if result.MinLength, err = copyInt64(schema.MinLength); err != nil {
		return nil, err
	}
	if result.MaxLength, err = copyInt64(schema.MaxLength); err != nil {
		return nil, err
	}
	if result.Minimum, err = copyFloat64(schema.Minimum); err != nil {
		return nil, err
	}
	if result.Maximum, err = copyFloat64(schema.Maximum); err != nil {
		return nil, err
	}
	if schema.Items != nil {
		result.Items, err = convertJSONSchemaToGenai(schema.Items)
		if err != nil {
			return nil, err
		}
	}
	if len(schema.Properties) != 0 {
		result.Properties = make(map[string]*genai.Schema, len(schema.Properties))
		for name, property := range schema.Properties {
			converted, err := convertJSONSchemaToGenai(property)
			if err != nil {
				return nil, fmt.Errorf("property %q: %w", name, err)
			}
			result.Properties[name] = converted
		}
	}
	result.Required = append([]string(nil), schema.Required...)
	return result, nil
}

func genaiType(value string) (genai.Type, error) {
	switch value {
	case "string":
		return genai.TypeString, nil
	case "number":
		return genai.TypeNumber, nil
	case "integer":
		return genai.TypeInteger, nil
	case "boolean":
		return genai.TypeBoolean, nil
	case "array":
		return genai.TypeArray, nil
	case "object":
		return genai.TypeObject, nil
	default:
		return "", fmt.Errorf("unsupported schema type %q", value)
	}
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type deploymentDigestInput struct {
	ManifestDigest string             `json:"manifestDigest"`
	Agents         []agentDigestInput `json:"agents"`
}

type agentDigestInput struct {
	Name              string           `json:"name"`
	Version           string           `json:"version"`
	Description       string           `json:"description"`
	Mode              AgentMode        `json:"mode"`
	SubAgents         []AgentReference `json:"subAgents"`
	InstructionDigest string           `json:"instructionDigest"`
	SchemaDigest      string           `json:"schemaDigest,omitempty"`
	TextPointer       string           `json:"textPointer,omitempty"`
	Tools             []string         `json:"tools"`
}

func agentDigestInputFrom(definition *AgentDefinition) agentDigestInput {
	tools := append([]string(nil), definition.Tools.Allow...)
	slices.Sort(tools)
	return agentDigestInput{
		Name:              definition.Name,
		Version:           definition.Version,
		Description:       definition.Description,
		Mode:              definition.Mode,
		SubAgents:         append([]AgentReference(nil), definition.SubAgents...),
		InstructionDigest: definition.InstructionDigest,
		SchemaDigest:      definition.SchemaDigest,
		TextPointer:       definition.Output.TextPointer,
		Tools:             tools,
	}
}

func agentDigest(definition AgentDefinition) (string, error) {
	raw, err := json.Marshal(agentDigestInputFrom(&definition))
	if err != nil {
		return "", fmt.Errorf("marshal agent digest input: %w", err)
	}
	return sha256Hex(raw), nil
}

func deploymentDigest(manifest []byte, deployment *Deployment) (string, error) {
	agents := make([]agentDigestInput, 0, len(deployment.Spec.Agents))
	for index := range deployment.Spec.Agents {
		agents = append(agents, agentDigestInputFrom(&deployment.Spec.Agents[index]))
	}
	raw, err := json.Marshal(deploymentDigestInput{
		ManifestDigest: sha256Hex(manifest),
		Agents:         agents,
	})
	if err != nil {
		return "", fmt.Errorf("marshal deployment digest input: %w", err)
	}
	return sha256Hex(raw), nil
}

// EntrypointDefinition returns the sole root of the ADK Agent tree.
func (d *Deployment) EntrypointDefinition() (*AgentDefinition, error) {
	root, err := d.RootName()
	if err != nil {
		return nil, err
	}
	for index := range d.Spec.Agents {
		if d.Spec.Agents[index].Name == root {
			return &d.Spec.Agents[index], nil
		}
	}
	return nil, fmt.Errorf("root agent %q is not defined", root)
}

// RootName derives the entrypoint from the ADK Agent tree rather than from a
// duplicate deployment field.
func (d *Deployment) RootName() (string, error) {
	incoming := make(map[string]int, len(d.Spec.Agents))
	for _, definition := range d.Spec.Agents {
		incoming[definition.Name] += 0
		for _, reference := range definition.SubAgents {
			incoming[reference.Name]++
		}
	}
	var roots []string
	for _, definition := range d.Spec.Agents {
		if incoming[definition.Name] == 0 {
			roots = append(roots, definition.Name)
		}
	}
	if len(roots) != 1 {
		return "", fmt.Errorf("deployment must have exactly one root Agent, got %d", len(roots))
	}
	return roots[0], nil
}

func (d *Deployment) Agent(name string) (*AgentDefinition, bool) {
	for index := range d.Spec.Agents {
		if d.Spec.Agents[index].Name == name {
			return &d.Spec.Agents[index], true
		}
	}
	return nil, false
}

// DefinitionRef returns the deployment-level identity used by audit and MCP.
func (d *Deployment) DefinitionRef() DefinitionRef {
	return DefinitionRef{
		Name:    d.Metadata.Name,
		Version: d.Metadata.Version,
		Digest:  d.Digest,
	}
}

// Ensure compile-time compatibility with ADK's Agent description surface.
var _ = llmagent.Config{}
