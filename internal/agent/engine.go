package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ml8s/liki-agents/internal/platform"
	"github.com/ml8s/liki-agents/internal/platform/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
)

// engineMCPProtocolVersion returns the latest stateless MCP revision supported
// by the official SDK. Server/discover is the health mechanism; ping and the
// legacy HTTP health endpoint are deliberately not used.
func engineMCPProtocolVersion() string {
	return mcp.SupportedProtocolVersions()[0]
}

// CheckHealth verifies Engine readiness with the MCP protocol's own discovery
// RPC. This validates the stateless request path, protocol revision, server
// identity, and the tool capability the runtime actually depends on.
func (r *Runtime) CheckHealth(ctx context.Context) platform.DependencyHealth {
	ctx, cancel := context.WithTimeout(ctx, r.config.EngineTimeout)
	defer cancel()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "liki-agents", Version: buildinfo.Version},
		nil,
	)
	var transport mcp.Transport = newEngineTransport(r.config)
	if r.config.engineTransportOverride != nil {
		transport = r.config.engineTransportOverride
	}
	session, err := client.Connect(
		ctx,
		transport,
		&mcp.ClientSessionOptions{ProtocolVersion: engineMCPProtocolVersion()},
	)
	if err != nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: healthDetail(err)}
	}
	defer session.Close()

	result := session.InitializeResult()
	if result == nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine discovery returned no result"}
	}
	if result.ProtocolVersion != engineMCPProtocolVersion() {
		return platform.DependencyHealth{
			Name:   "engine_mcp",
			OK:     false,
			Detail: fmt.Sprintf("engine negotiated MCP %s, want %s", result.ProtocolVersion, engineMCPProtocolVersion()),
		}
	}
	if result.ServerInfo == nil || result.ServerInfo.Name == "" {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine discovery omitted server identity"}
	}
	if result.Capabilities == nil || result.Capabilities.Tools == nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine does not advertise tool capability"}
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: healthDetail(err)}
	}
	advertised := make(map[string]struct{}, len(listed.Tools))
	for _, tool := range listed.Tools {
		advertised[tool.Name] = struct{}{}
	}
	definitions := make([]AgentDefinition, 0)
	if r.config.Deployment != nil {
		definitions = r.config.Deployment.Spec.Agents
	} else if r.entrypoint != nil {
		definitions = []AgentDefinition{*r.entrypoint}
	}
	for _, definition := range definitions {
		for _, tool := range definition.Tools.Allow {
			if _, ok := advertised[tool]; !ok {
				return platform.DependencyHealth{
					Name:   "engine_mcp",
					OK:     false,
					Detail: fmt.Sprintf("Engine does not advertise required tool %q for agent %q", tool, definition.Name),
				}
			}
		}
	}
	return platform.DependencyHealth{Name: "engine_mcp", OK: true}
}

func healthDetail(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return err.Error()
}

func newEngineTransport(config Config) *mcp.StreamableClientTransport {
	httpClient := &http.Client{Timeout: config.EngineTimeout, Transport: http.DefaultTransport}
	if config.EngineToken != "" {
		httpClient.Transport = &auth.Transport{
			Provider: auth.StaticToken(config.EngineToken),
			Base:     http.DefaultTransport,
		}
	}
	return &mcp.StreamableClientTransport{
		Endpoint:             config.EngineMCPURL,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
}

func newEngineToolTransport(config Config) mcp.Transport {
	if config.engineTransportOverride != nil {
		return config.engineTransportOverride
	}
	return newEngineTransport(config)
}

var _ platform.HealthChecker = (*Runtime)(nil)
