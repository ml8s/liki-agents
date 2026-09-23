package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/liki/liki-agent/internal/platform"
	"github.com/liki/liki-agent/internal/platform/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
)

// engineMCPProtocolVersion is the current stateless MCP revision used by the
// deployed Engine. The official SDK performs server/discover; ping and the
// legacy HTTP health endpoint are deliberately not used.
const engineMCPProtocolVersion = "2026-07-28"

// CheckHealth verifies Engine readiness with the MCP protocol's own discovery
// RPC. This validates the stateless request path, protocol revision, server
// identity, and the tool capability the runtime actually depends on.
func (r *Runtime) CheckHealth(ctx context.Context) platform.DependencyHealth {
	ctx, cancel := context.WithTimeout(ctx, r.config.EngineTimeout)
	defer cancel()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "liki-agent", Version: buildinfo.Version},
		nil,
	)
	var transport mcp.Transport = newEngineTransport(r.config)
	if r.config.engineTransportOverride != nil {
		transport = r.config.engineTransportOverride
	}
	session, err := client.Connect(
		ctx,
		transport,
		&mcp.ClientSessionOptions{ProtocolVersion: engineMCPProtocolVersion},
	)
	if err != nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: healthDetail(err)}
	}
	defer session.Close()

	result := session.InitializeResult()
	if result == nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine discovery returned no result"}
	}
	if result.ProtocolVersion != engineMCPProtocolVersion {
		return platform.DependencyHealth{
			Name:   "engine_mcp",
			OK:     false,
			Detail: fmt.Sprintf("engine negotiated MCP %s, want %s", result.ProtocolVersion, engineMCPProtocolVersion),
		}
	}
	if result.ServerInfo == nil || result.ServerInfo.Name == "" {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine discovery omitted server identity"}
	}
	if result.Capabilities == nil || result.Capabilities.Tools == nil {
		return platform.DependencyHealth{Name: "engine_mcp", OK: false, Detail: "engine does not advertise tool capability"}
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

var _ platform.HealthChecker = (*Runtime)(nil)

var _ platform.HealthChecker = (*Runtime)(nil)
