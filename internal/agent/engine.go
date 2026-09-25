// Package agent contains MCP dependency health. The official MCP SDK owns
// protocol behavior; the runtime owns deployment-aware readiness.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ml8s/liki-agents/internal/platform"
	"github.com/ml8s/liki-agents/internal/platform/buildinfo"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/auth"
)

// mcpProtocolVersion returns the latest stateless MCP revision supported by
// the official SDK. Server discovery is the health mechanism; ping and legacy
// HTTP health endpoints are deliberately not used.
func mcpProtocolVersion() string {
	return mcp.SupportedProtocolVersions()[0]
}

// CheckHealth verifies every deployment-declared MCP dependency with the MCP
// protocol's own discovery RPC. This validates the stateless request path,
// protocol revision, server identity, and required tool capability.
func (r *Runtime) CheckHealth(ctx context.Context) platform.DependencyHealth {
	servers, err := resolveMCPServers(r.deploymentMCPServers())
	if err != nil {
		return platform.DependencyHealth{Name: "mcp", OK: false, Detail: healthDetail(err)}
	}
	if len(servers) == 0 {
		return platform.DependencyHealth{Name: "mcp", OK: true}
	}

	failures := make([]string, 0)
	for _, server := range servers {
		if err := r.checkMCPDependency(ctx, server); err != nil {
			failures = append(failures, server.definition.Name+": "+healthDetail(err))
		}
	}
	if len(failures) != 0 {
		return platform.DependencyHealth{Name: "mcp", OK: false, Detail: strings.Join(failures, "; ")}
	}
	return platform.DependencyHealth{Name: "mcp", OK: true}
}

// MCPHealthChecks returns one readiness checker per declared logical MCP
// server. Composite readiness cannot hide an individual dependency failure.
func (r *Runtime) MCPHealthChecks() []platform.HealthChecker {
	definitions := r.deploymentMCPServers()
	checkers := make([]platform.HealthChecker, 0, len(definitions))
	for _, definition := range definitions {
		checkers = append(checkers, &mcpServerHealthChecker{
			runtime:    r,
			definition: definition,
		})
	}
	return checkers
}

type mcpServerHealthChecker struct {
	runtime    *Runtime
	definition MCPServerDefinition
}

func (c *mcpServerHealthChecker) CheckHealth(ctx context.Context) platform.DependencyHealth {
	name := "mcp/" + c.definition.Name
	resolved, err := resolveMCPServer(c.definition)
	if err != nil {
		return platform.DependencyHealth{Name: name, OK: false, Detail: healthDetail(err)}
	}
	if err := c.runtime.checkMCPDependency(ctx, resolved); err != nil {
		return platform.DependencyHealth{Name: name, OK: false, Detail: healthDetail(err)}
	}
	return platform.DependencyHealth{Name: name, OK: true}
}

func checkMCPDependencyHealth(
	ctx context.Context,
	config Config,
	server resolvedMCPServer,
	requiredTools []ToolReference,
) error {
	if _, overridden := config.mcpTransportOverrides[server.definition.Name]; !overridden {
		if err := validateMCPEndpoint(server); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, config.MCPTimeout)
	defer cancel()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "liki-agents", Version: buildinfo.Version},
		nil,
	)
	var transport mcp.Transport = newMCPTransport(config, server)
	if override, ok := config.mcpTransportOverrides[server.definition.Name]; ok {
		transport = override
	}
	session, err := client.Connect(
		ctx,
		transport,
		&mcp.ClientSessionOptions{ProtocolVersion: mcpProtocolVersion()},
	)
	if err != nil {
		return err
	}
	defer session.Close()

	result := session.InitializeResult()
	if result == nil {
		return errors.New("discovery returned no result")
	}
	if result.ProtocolVersion != mcpProtocolVersion() {
		return fmt.Errorf("negotiated MCP %s, want %s", result.ProtocolVersion, mcpProtocolVersion())
	}
	if result.ServerInfo == nil || result.ServerInfo.Name == "" {
		return errors.New("discovery omitted server identity")
	}
	if result.Capabilities == nil || result.Capabilities.Tools == nil {
		return errors.New("server does not advertise tool capability")
	}
	listed, err := session.ListTools(ctx, nil)
	if err != nil {
		return err
	}
	advertised := make(map[string]struct{}, len(listed.Tools))
	for _, tool := range listed.Tools {
		advertised[tool.Name] = struct{}{}
	}
	for _, tool := range requiredTools {
		if _, ok := advertised[tool.Name]; !ok {
			return fmt.Errorf("missing required tool %q", tool.Name)
		}
	}
	return nil
}

func (r *Runtime) deploymentMCPServers() []MCPServerDefinition {
	if r.config.Deployment == nil {
		return nil
	}
	return r.config.Deployment.Spec.MCPServers
}

func deploymentToolReferences(deployment *Deployment, server string) []ToolReference {
	if deployment == nil {
		return nil
	}
	references := make([]ToolReference, 0)
	for _, definition := range deployment.Spec.Agents {
		for _, reference := range definition.Tools.References() {
			if reference.Server == server {
				references = append(references, reference)
			}
		}
	}
	return references
}

func (r *Runtime) checkMCPDependency(
	ctx context.Context,
	server resolvedMCPServer,
) error {
	return checkMCPDependencyHealth(
		ctx,
		r.config,
		server,
		deploymentToolReferences(r.config.Deployment, server.definition.Name),
	)
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

func newMCPTransport(config Config, server resolvedMCPServer) *mcp.StreamableClientTransport {
	httpClient := &http.Client{Timeout: config.MCPTimeout, Transport: http.DefaultTransport}
	if server.Token != "" {
		httpClient.Transport = &auth.Transport{
			Provider: auth.StaticToken(server.Token),
			Base:     http.DefaultTransport,
		}
	}
	return &mcp.StreamableClientTransport{
		Endpoint:             server.Endpoint,
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
}

var _ platform.HealthChecker = (*Runtime)(nil)
