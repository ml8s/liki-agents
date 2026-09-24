package agent

import (
	"encoding/json"
	"testing"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

type staticToolset struct{}

func (staticToolset) Name() string { return "static" }

func (staticToolset) Tools(adkagent.ReadonlyContext) ([]tool.Tool, error) {
	return nil, nil
}

func TestAgentDefinitionCompilesToStandardADKConfig(t *testing.T) {
	temperature := float32(0.25)
	rawSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
	}
	definition := AgentDefinition{
		Name:              "worker",
		Description:       "generic worker",
		Mode:              AgentModeTask,
		InstructionText:   "generic instruction",
		GenaiOutputSchema: &genai.Schema{Type: genai.TypeObject},
		RawOutputSchema:   rawSchema,
		Tools:             ToolAllowlist{Allow: []string{"engine_tool"}},
	}

	beforeModel := func(adkagent.Context, *model.LLMRequest) (*model.LLMResponse, error) { return nil, nil }
	config := definition.ADKConfig(ADKAgentRuntime{
		RawOutputSchema:      rawSchema,
		Toolset:              staticToolset{},
		SubAgents:            []adkagent.Agent{nil},
		Temperature:          temperature,
		OutputKey:            "structured_output",
		IsEntrypoint:         true,
		BeforeModelCallbacks: []llmagent.BeforeModelCallback{beforeModel},
	})

	if config.Name != "worker" || config.Description != "generic worker" {
		t.Fatalf("identity = %q/%q", config.Name, config.Description)
	}
	if config.Mode != llmagent.ModeTask || config.Instruction != "generic instruction" {
		t.Fatalf("mode/instruction = %v/%q", config.Mode, config.Instruction)
	}
	if config.Model != nil || len(config.SubAgents) != 1 {
		t.Fatalf("runtime dependencies = %#v", config)
	}
	if len(config.Toolsets) != 1 || config.Toolsets[0].Name() != "static" {
		t.Fatalf("toolsets = %#v", config.Toolsets)
	}
	if config.GenerateContentConfig == nil || config.GenerateContentConfig.Temperature == nil ||
		*config.GenerateContentConfig.Temperature != temperature {
		t.Fatalf("generation config = %#v", config.GenerateContentConfig)
	}
	if config.OutputSchema != definition.GenaiOutputSchema || config.OutputKey != "structured_output" {
		t.Fatalf("structured output = %#v/%q", config.OutputSchema, config.OutputKey)
	}
	if !config.DisallowTransferToParent || !config.DisallowTransferToPeers {
		t.Fatalf("transfer policy = parent:%v peers:%v", config.DisallowTransferToParent, config.DisallowTransferToPeers)
	}
	if len(config.BeforeModelCallbacks) != 2 {
		t.Fatalf("callbacks = %#v", config.BeforeModelCallbacks)
	}
}

func TestRawJSONSchemaIsUsedOnStandardOpenAICompatibleWire(t *testing.T) {
	raw := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           map[string]any{"answer": map[string]any{"type": "string"}},
	}
	callback := rawOutputSchemaCallback(raw)
	request := &model.LLMRequest{Config: &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   &genai.Schema{Type: genai.TypeObject},
	}}

	if _, err := callback(nil, request); err != nil {
		t.Fatal(err)
	}
	if request.Config.ResponseSchema != nil {
		t.Fatal("lossy genai schema was sent to provider")
	}
	if request.Config.ResponseJsonSchema == nil {
		t.Fatal("raw JSON Schema was not sent to provider")
	}
	encoded, err := json.Marshal(request.Config.ResponseJsonSchema)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoded) {
		t.Fatalf("raw schema is invalid JSON: %s", encoded)
	}
}
