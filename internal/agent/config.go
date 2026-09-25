package agent

import (
	"log/slog"
	"time"

	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/adk/v2/model"
)

type Config struct {
	AppName           string
	Model             string
	ModelBaseURL      string
	ModelAPIKey       string
	ModelTimeout      time.Duration
	Temperature       float64
	AuditRecorder     audit.Recorder
	Metrics           Metrics
	Provider          string
	StructuredOutput  string
	ContractVersion   string
	GraphVersion      string
	Deployment        *Deployment
	MCPTimeout        time.Duration
	MaxConcurrentRuns int
	Now               func() time.Time
	Logger            *slog.Logger
	TracerProvider    trace.TracerProvider

	// modelOverride is an internal test seam. Production builds always
	// construct the OpenAI-compatible provider in NewRuntime.
	modelOverride model.LLM

	// mcpTransportOverrides is an internal test seam for protocol health
	// tests. Production always uses the official Streamable HTTP transport.
	mcpTransportOverrides map[string]mcp.Transport
}
