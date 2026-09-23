package agent

import (
	"context"
	"errors"
	"iter"
	"strings"
	"testing"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

type capturingLLM struct {
	request  *model.LLMRequest
	response *model.LLMResponse
}

func (m *capturingLLM) Name() string { return "capturing-model" }

func (m *capturingLLM) GenerateContent(
	_ context.Context,
	req *model.LLMRequest,
	_ bool,
) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.request = req
		yield(m.response, nil)
	}
}

func TestJSONObjectModelKeepsOneStructuredContractOnJSONMode(t *testing.T) {
	inner := &capturingLLM{response: &model.LLMResponse{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "{}"}}},
	}}
	adapter, err := newJSONObjectModel(inner, structuredOutputSchema())
	if err != nil {
		t.Fatalf("newJSONObjectModel() error = %v", err)
	}
	if adapter.Name() != inner.Name() {
		t.Fatalf("Name() = %q, want %q", adapter.Name(), inner.Name())
	}
	schema := structuredOutputSchema()
	request := &model.LLMRequest{
		Model: "glm-5.3-flash",
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("You are a destiny analyst.", systemRole),
			ResponseSchema:    schema,
			ResponseMIMEType:  "application/json",
		},
	}

	responses := 0
	for response, err := range adapter.GenerateContent(context.Background(), request, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		if response == nil {
			t.Fatal("GenerateContent() response = nil")
		}
		responses++
	}
	if responses != 1 {
		t.Fatalf("responses = %d, want 1", responses)
	}

	if request.Config.ResponseSchema != schema || request.Config.ResponseMIMEType != "application/json" {
		t.Fatal("adapter mutated the caller request")
	}
	adapted := inner.request
	if adapted == nil || adapted.Config == nil {
		t.Fatal("delegate did not receive adapted request config")
	}
	if adapted.Config.ResponseSchema != nil || adapted.Config.ResponseJsonSchema != nil {
		t.Fatal("provider-unsupported response schema was not removed")
	}
	if adapted.Config.ResponseMIMEType != "application/json" {
		t.Fatalf("response MIME type = %q, want application/json", adapted.Config.ResponseMIMEType)
	}
	instruction := textFromContent(adapted.Config.SystemInstruction)
	if !strings.Contains(instruction, "You are a destiny analyst.") {
		t.Fatal("existing system instruction was lost")
	}
	for _, marker := range []string{"answer", "confidence", "topic", "key_factors", "limitations"} {
		if !strings.Contains(instruction, marker) {
			t.Fatalf("injected schema missing %q: %q", marker, instruction)
		}
	}
	if !strings.Contains(instruction, structuredOutputInstructionPrefix) {
		t.Fatal("JSON-only output instruction was not injected")
	}
}

func TestJSONObjectModelRejectsInvalidConstruction(t *testing.T) {
	schema := structuredOutputSchema()
	validModel := &capturingLLM{}
	testCases := []struct {
		name    string
		inner   model.LLM
		schema  *genai.Schema
		wantErr error
	}{
		{name: "nil delegate", inner: nil, schema: schema, wantErr: domain.ErrInvalidInput},
		{name: "nil schema", inner: validModel, schema: nil, wantErr: domain.ErrInvalidInput},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := newJSONObjectModel(testCase.inner, testCase.schema)
			if !errors.Is(err, testCase.wantErr) {
				t.Fatalf("newJSONObjectModel() error = %v, want %v", err, testCase.wantErr)
			}
		})
	}
}

func TestProviderUsesJSONObjectOutput(t *testing.T) {
	testCases := []struct {
		provider string
		baseURL  string
		want     bool
	}{
		{provider: "zhipu", want: true},
		{provider: "BigModel", want: true},
		{provider: "glm", want: true},
		{provider: "", baseURL: "https://open.bigmodel.cn/api/v1", want: true},
		{provider: "", baseURL: "https://api.openai.com/v1", want: false},
		{provider: "openai-compatible", baseURL: "https://open.bigmodel.cn/api/v1", want: false},
	}
	for _, testCase := range testCases {
		if got := providerUsesJSONObjectOutput(testCase.provider, testCase.baseURL); got != testCase.want {
			t.Fatalf(
				"providerUsesJSONObjectOutput(%q, %q) = %v, want %v",
				testCase.provider, testCase.baseURL, got, testCase.want,
			)
		}
	}
}

func TestJSONObjectModelNormalizesCommentaryAroundJSONPayload(t *testing.T) {
	payload := `{"answer":"八字显示倾向而非确定。","confidence":0.8,"topic":"career","key_factors":["七杀"],"limitations":["非决定性"]}`
	inner := &capturingLLM{response: &model.LLMResponse{
		Content: &genai.Content{Role: "model", Parts: []*genai.Part{
			{Text: "I have the chart facts now.", Thought: true},
			{Text: "以下是结构化结果：\n" + payload + "\n谢谢。"},
		}},
	}}
	adapter, err := newJSONObjectModel(inner, structuredOutputSchema())
	if err != nil {
		t.Fatalf("newJSONObjectModel() error = %v", err)
	}
	request := &model.LLMRequest{Config: &genai.GenerateContentConfig{ResponseSchema: structuredOutputSchema()}}
	var normalizedResponse *model.LLMResponse
	for response, err := range adapter.GenerateContent(context.Background(), request, false) {
		if err != nil {
			t.Fatalf("GenerateContent() error = %v", err)
		}
		normalizedResponse = response
	}
	response := normalizedResponse
	if response == nil || response.Content == nil {
		t.Fatal("delegate response was missing")
	}
	var payloadParts []*genai.Part
	for _, part := range response.Content.Parts {
		if part != nil && part.Text != "" && !part.Thought {
			payloadParts = append(payloadParts, part)
		}
	}
	if len(payloadParts) != 1 || payloadParts[0].Text != payload {
		t.Fatalf("payload parts = %#v, want one normalized JSON part", payloadParts)
	}
}

func TestExtractJSONObjectIgnoresBracesInStrings(t *testing.T) {
	payload := `{"answer":"包含 { braces } 和\"引号\"。","confidence":0.7,"topic":"general","key_factors":[],"limitations":[]}`
	raw := "说明\n" + payload + "\n结尾"
	got, ok := extractJSONObject(raw)
	if !ok || got != payload {
		t.Fatalf("extractJSONObject() = %q, %v", got, ok)
	}
	if _, ok := extractJSONObject("没有对象"); ok {
		t.Fatal("extractJSONObject() accepted text without a JSON object")
	}
}

func TestVisibleEventDropsPartialToolCallDuplicates(t *testing.T) {
	partial := &session.Event{
		LLMResponse: model.LLMResponse{
			Partial: true,
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "tianwen_time"}},
			}},
		},
	}
	final := &session.Event{
		LLMResponse: model.LLMResponse{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_1", Name: "tianwen_time"}},
			}},
		},
	}
	if visible := visibleEvent(partial); visible != nil {
		t.Fatalf("visibleEvent(partial) = %+v, want nil", visible)
	}
	visible := visibleEvent(final)
	if visible == nil || len(visible.Content.Parts) != 1 || visible.Content.Parts[0].FunctionCall == nil {
		t.Fatalf("visibleEvent(final) = %+v, want one function call", visible)
	}
}
