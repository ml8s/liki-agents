// Package a2a exposes the Liki ADK runtime through the official A2A protocol
// binding. There is no private agent API behind this adapter.
package a2a

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/liki/liki-agent/internal/agent"
	"github.com/liki/liki-agent/internal/platform/buildinfo"
	"github.com/liki/liki-agent/internal/platform/identity"
	"google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

const (
	// Path is the canonical JSON-RPC binding declared by the Agent Card.
	Path = "/a2a"
)

// Config owns the public transport declaration for A2A discovery.
type Config struct {
	PublicURL  *url.URL
	RunTimeout time.Duration
}

// Server is the official ADK/A2A executor and Agent Card provider.
type Server struct {
	card      a2a.AgentCard
	mux       *http.ServeMux
	endpoint  http.Handler
	discovery http.Handler
}

// New wires the ADK runtime directly into the official A2A server stack.
func New(runtime *agent.Runtime, config Config) (*Server, error) {
	if runtime == nil {
		return nil, fmt.Errorf("adk runtime is required")
	}
	if config.PublicURL == nil {
		return nil, fmt.Errorf("public url is required")
	}
	root := runtime.RootAgent()
	capabilities := a2a.AgentCapabilities{Streaming: true}
	securityScheme := a2a.HTTPAuthSecurityScheme{
		Scheme:       "Bearer",
		BearerFormat: "opaque",
		Description:  "Liki internal service bearer token",
	}
	card := a2a.AgentCard{
		Name:        root.Name(),
		Description: root.Description(),
		Version:     buildinfo.Version,
		SupportedInterfaces: []*a2a.AgentInterface{
			a2a.NewAgentInterface(config.PublicURL.JoinPath(Path).String(), a2a.TransportProtocolJSONRPC),
		},
		Capabilities: capabilities,
		SecuritySchemes: a2a.NamedSecuritySchemes{
			"liki_service_bearer": securityScheme,
		},
		SecurityRequirements: a2a.SecurityRequirementsOptions{
			{"liki_service_bearer": {}},
		},
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(root),
	}

	executor := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runtime.RunnerConfig(),
		GenAIPartConverter: func(_ context.Context, event *session.Event, part *genai.Part) (*a2a.Part, error) {
			return structuredAwarePart(event, part)
		},
		BeforeExecuteCallback: func(ctx context.Context, request *a2asrv.ExecutorContext) (context.Context, error) {
			caller := ""
			if verified, ok := identity.UserIDFromContext(ctx); ok {
				caller = verified
			}
			if caller == "" && request.User != nil && request.User.Authenticated {
				caller = request.User.Name
			}
			if caller == "" {
				caller = "a2a:" + string(request.TaskID)
			}
			scope := agent.AuditRunScope{
				RunID:    string(request.TaskID),
				ThreadID: request.ContextID,
				UserID:   caller,
				Product:  metadataString(request.Metadata, "product", "liki-agent"),
			}
			if err := runtime.BeginAuditRun(request.ContextID, scope); err != nil {
				return nil, err
			}
			return ctx, nil
		},
		AfterExecuteCallback: func(ctx adka2a.ExecutorContext, _ *a2a.TaskStatusUpdateEvent, err error) error {
			runtime.EndAuditRun(ctx.SessionID(), err)
			return nil
		},
		A2AExecutionCleanupCallback: func(ctx context.Context, request *a2asrv.ExecutorContext, _ []*a2a.AgentCard, _ a2a.SendMessageResult, cause error) {
			runtime.EndAuditRun(request.ContextID, cause)
		},
	})
	handlerOptions := []a2asrv.RequestHandlerOption{a2asrv.WithCapabilityChecks(&capabilities)}
	if config.RunTimeout > 0 {
		handlerOptions = append(handlerOptions, a2asrv.WithAgentInactivityTimeout(config.RunTimeout))
	}
	requestHandler := a2asrv.NewHandler(
		executor,
		handlerOptions...,
	)

	mux := http.NewServeMux()
	discovery := a2asrv.NewStaticAgentCardHandler(&card)
	endpoint := a2asrv.NewJSONRPCHandler(requestHandler)
	mux.Handle(a2asrv.WellKnownAgentCardPath, discovery)
	mux.Handle(Path, endpoint)
	return &Server{card: card, mux: mux, endpoint: endpoint, discovery: discovery}, nil
}

// AgentCard returns the A2A discovery document value.
func (s *Server) AgentCard() a2a.AgentCard {
	return s.card
}

// Handler serves Agent Card discovery and the canonical A2A JSON-RPC endpoint.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// AgentCardHandler serves the standard well-known discovery endpoint.
func (s *Server) AgentCardHandler() http.Handler {
	return s.discovery
}

// EndpointHandler serves the standard JSON-RPC protocol endpoint.
func (s *Server) EndpointHandler() http.Handler {
	return s.endpoint
}

func metadataString(value map[string]any, key, fallback string) string {
	if text, ok := value[key].(string); ok && strings.TrimSpace(text) != "" {
		return text
	}
	return fallback
}

// structuredAwarePart maps native ADK parts onto A2A artifact parts. Model
// text in the structured-output graph is JSON payload, so partial text chunks
// are dropped and the final text part carries the parsed user-facing answer.
// Tool call and response facts keep the framework's default mapping.
func structuredAwarePart(event *session.Event, part *genai.Part) (*a2a.Part, error) {
	if part == nil {
		return nil, nil
	}
	if part.FunctionCall != nil || part.FunctionResponse != nil {
		var longRunningToolIDs []string
		if event != nil {
			longRunningToolIDs = event.LongRunningToolIDs
		}
		return adka2a.ToA2APart(part, longRunningToolIDs)
	}
	if event != nil && event.IsFinalResponse() {
		if answer := structuredAnswer(event); answer != "" {
			return a2a.NewTextPart(answer), nil
		}
	}
	return nil, nil
}

func structuredAnswer(event *session.Event) string {
	if event == nil || event.Actions.StateDelta == nil {
		return ""
	}
	value, ok := event.Actions.StateDelta[agent.StructuredOutputStateKey]
	if !ok {
		return ""
	}
	analysis, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	answer, _ := analysis["answer"].(string)
	return strings.TrimSpace(answer)
}
