// chat_model.go implements the ADK model.LLM interface using the standard
// OpenAI Chat Completions API (POST /chat/completions). Unlike ADK's built-in
// openaimodel (which uses the Responses API, only available on OpenAI), this
// adapter works with every OpenAI-compatible provider: Zhipu, DeepSeek,
// Moonshot, Ollama, vLLM, and others.
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"strings"
	"time"

	"github.com/liki/liki-agent/internal/domain"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

// ChatCompletionsModel is an ADK model.LLM implementation backed by the
// standard OpenAI Chat Completions endpoint.
type ChatCompletionsModel struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewChatCompletionsModel creates a model that talks to any OpenAI-compatible
// Chat Completions endpoint.
func NewChatCompletionsModel(name, baseURL, apiKey string, timeout time.Duration) *ChatCompletionsModel {
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	return &ChatCompletionsModel{
		name:    name,
		baseURL: baseURL + "chat/completions",
		apiKey:  apiKey,
		client:  &http.Client{Timeout: timeout},
	}
}

func (m *ChatCompletionsModel) Name() string { return m.name }

// chatMessage is one entry in the Chat Completions messages array.
type chatMessage struct {
	Role       string         `json:"role"`
	Content    any            `json:"content,omitempty"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

type chatToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type chatTool struct {
	Type     string       `json:"type"`
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Tools       []chatTool    `json:"tools,omitempty"`
	Temperature *float64      `json:"temperature,omitempty"`
	MaxTokens   *int64        `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage *chatUsage `json:"usage,omitempty"`
}

type chatUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// GenerateContent implements model.LLM. It always returns a complete
// (non-streaming) response regardless of the stream flag, because the
// Chat Completions API returns all function calls in one response.
func (m *ChatCompletionsModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		httpReq, err := m.buildRequest(ctx, req)
		if err != nil {
			yield(nil, domain.NewError(domain.CodeLLMRequestInvalid, "build chat completions request", err))
			return
		}
		httpResp, err := m.client.Do(httpReq)
		if err != nil {
			yield(nil, domain.NewError(domain.CodeLLMUnavailable, "chat completions request failed", err))
			return
		}
		defer httpResp.Body.Close()

		if httpResp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(httpResp.Body)
			yield(nil, domain.NewError(domain.CodeLLMCallFailed, fmt.Sprintf("chat completions returned %d: %s", httpResp.StatusCode, string(body)), nil))
			return
		}

		var resp chatResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
			yield(nil, domain.NewError(domain.CodeLLMResponseInvalid, "decode chat completions response", err))
			return
		}

		if len(resp.Choices) == 0 {
			yield(nil, domain.NewError(domain.CodeRuntimeEmptyResponse, "chat completions returned no choices", nil))
			return
		}

		yield(m.toLLMResponse(&resp, req.Model), nil)
	}
}

func (m *ChatCompletionsModel) buildRequest(ctx context.Context, req *model.LLMRequest) (*http.Request, error) {
	chatReq := chatRequest{Model: m.name}
	chatReq.Model = req.Model

	// System instruction
	if req.Config != nil && req.Config.SystemInstruction != nil {
		for _, part := range req.Config.SystemInstruction.Parts {
			if part.Text != "" {
				chatReq.Messages = append(chatReq.Messages, chatMessage{Role: "system", Content: part.Text})
				break
			}
		}
	}

	// Conversation contents
	for _, content := range req.Contents {
		role := content.Role
		if role == "model" {
			role = "assistant"
		}
		msg := chatMessage{Role: role}
		var textParts []string
		for _, part := range content.Parts {
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			}
			if part.FunctionCall != nil {
				tc := chatToolCall{ID: part.FunctionCall.ID, Type: "function"}
				tc.Function.Name = part.FunctionCall.Name
				args, _ := json.Marshal(part.FunctionCall.Args)
				tc.Function.Arguments = string(args)
				msg.ToolCalls = append(msg.ToolCalls, tc)
			}
			if part.FunctionResponse != nil {
				respJSON, _ := json.Marshal(part.FunctionResponse.Response)
				chatReq.Messages = append(chatReq.Messages, chatMessage{
					Role: "tool", Content: string(respJSON), ToolCallID: part.FunctionResponse.ID,
				})
				msg = chatMessage{Role: role} // reset msg for next non-tool part
				continue
			}
		}
		if len(textParts) > 0 {
			msg.Content = strings.Join(textParts, "")
		}
		if msg.Content != nil || len(msg.ToolCalls) > 0 {
			chatReq.Messages = append(chatReq.Messages, msg)
		}
	}

	// Tools
	if req.Config != nil && len(req.Config.Tools) > 0 {
		for _, tool := range req.Config.Tools {
			if fd := tool.FunctionDeclarations; fd != nil {
				for _, decl := range fd {
					ct := chatTool{Type: "function", Function: chatToolFunc{
						Name:        decl.Name,
						Description: decl.Description,
					}}
					if decl.Parameters != nil {
						ct.Function.Parameters = schemaToMap(decl.Parameters)
					}
					chatReq.Tools = append(chatReq.Tools, ct)
				}
			}
		}
	}

	// Temperature
	if req.Config != nil && req.Config.Temperature != nil {
		t := float64(*req.Config.Temperature)
		chatReq.Temperature = &t
	}

	body, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	return httpReq, nil
}

func (m *ChatCompletionsModel) toLLMResponse(resp *chatResponse, modelFallback string) *model.LLMResponse {
	choice := resp.Choices[0]
	content := &genai.Content{Role: "model"}

	if choice.Message.Content != nil {
		if text, ok := choice.Message.Content.(string); ok && text != "" {
			content.Parts = append(content.Parts, &genai.Part{Text: text})
		}
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		content.Parts = append(content.Parts, &genai.Part{
			FunctionCall: &genai.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args},
		})
	}

	llmResp := &model.LLMResponse{
		Content:      content,
		ModelVersion: modelFallback,
		TurnComplete: choice.FinishReason == "stop" || choice.FinishReason == "tool_calls",
	}

	if resp.Usage != nil {
		llmResp.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(resp.Usage.PromptTokens),
			CandidatesTokenCount: int32(resp.Usage.CompletionTokens),
			TotalTokenCount:      int32(resp.Usage.TotalTokens),
		}
	}

	return llmResp
}

func schemaToMap(s *genai.Schema) map[string]any {
	if s == nil {
		return nil
	}
	data, _ := json.Marshal(s)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result
}
