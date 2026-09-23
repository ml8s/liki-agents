package agent

import (
	"log/slog"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/model"
)

type Config struct {
	AppName          string
	AgentName        string
	AgentDescription string
	ExpertName       string
	System           string
	Model            string
	ModelBaseURL     string
	ModelAPIKey      string
	ModelTimeout     time.Duration
	Temperature      float64
	LLMRecorder      LLMCallRecorder
	Metrics          Metrics
	Provider         string
	Env              string
	ContractVersion  string
	GraphVersion     string
	PromptVersion    string
	PolicyVersion    string
	AllowedTools     []string
	EngineMCPURL     string
	EngineToken      string
	EngineTimeout    time.Duration
	Now              func() time.Time
	Logger           *slog.Logger

	// modelOverride is an internal test seam. Production builds always
	// construct the OpenAI-compatible provider in NewRuntime.
	modelOverride model.LLM

	// engineTransportOverride is an internal test seam for protocol health
	// tests. Production always uses the official Streamable HTTP transport.
	engineTransportOverride mcp.Transport
}
