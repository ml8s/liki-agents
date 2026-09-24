package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type healthProbe struct {
	sawDiscover      bool
	sawPing          bool
	requestedVersion string
}

func TestCheckHealthUsesCurrentMCPDiscovery(t *testing.T) {
	t.Parallel()

	probe, runtime := newHealthTestRuntime(t, &mcp.ServerOptions{}, "test_tool")
	health := runtime.CheckHealth(context.Background())
	if !health.OK {
		t.Fatalf("CheckHealth() = %+v, want ready", health)
	}
	if health.Name != "engine_mcp" {
		t.Fatalf("dependency name = %q", health.Name)
	}
	if !probe.sawDiscover {
		t.Fatal("engine did not receive server/discover")
	}
	if probe.sawPing {
		t.Error("health probe sent retired ping RPC")
	}
	if probe.requestedVersion != engineMCPProtocolVersion() {
		t.Fatalf("requested MCP version = %q, want %q", probe.requestedVersion, engineMCPProtocolVersion())
	}
}

func TestCheckHealthRejectsLegacyProtocolFallback(t *testing.T) {
	t.Parallel()

	options := &mcp.ServerOptions{
		SupportedProtocolVersions: []string{"2025-11-25"},
		Capabilities: &mcp.ServerCapabilities{
			Tools: &mcp.ToolCapabilities{},
		},
	}
	_, runtime := newHealthTestRuntime(t, options, "test_tool")
	health := runtime.CheckHealth(context.Background())
	if health.OK {
		t.Fatal("CheckHealth() accepted a legacy MCP negotiation")
	}
}

func TestCheckHealthRejectsMissingToolCapability(t *testing.T) {
	t.Parallel()

	options := &mcp.ServerOptions{Capabilities: &mcp.ServerCapabilities{}}
	_, runtime := newHealthTestRuntime(t, options, "")
	health := runtime.CheckHealth(context.Background())
	if health.OK {
		t.Fatal("CheckHealth() accepted an Engine without tool capability")
	}
}

func newHealthTestRuntime(t *testing.T, options *mcp.ServerOptions, toolName string) (*healthProbe, *Runtime) {
	t.Helper()

	probe := &healthProbe{}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	server := mcp.NewServer(
		&mcp.Implementation{Name: "test-engine", Version: "test"},
		options,
	)
	server.AddReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, request mcp.Request) (mcp.Result, error) {
			switch method {
			case "server/discover":
				probe.sawDiscover = true
				discover, ok := request.(*mcp.ServerRequest[*mcp.DiscoverParams])
				if !ok {
					t.Errorf("discover request has unexpected type %T", request)
					break
				}
				if version, _ := discover.Params.GetMeta()[mcp.MetaKeyProtocolVersion].(string); version != "" {
					probe.requestedVersion = version
				}
			case "ping":
				probe.sawPing = true
			}
			return next(ctx, method, request)
		}
	})
	if toolName != "" {
		server.AddTool(&mcp.Tool{
			Name:        toolName,
			Description: "test deterministic chart",
			InputSchema: &jsonschema.Schema{Type: "object"},
		}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{}, nil
		})
	}
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	return probe, &Runtime{config: Config{
		EngineTimeout:           5 * time.Second,
		engineTransportOverride: clientTransport,
	}, entrypoint: &AgentDefinition{Name: "test-agent", Tools: ToolAllowlist{Allow: []string{toolName}}}}
}

func TestCheckHealthRejectsMissingAllowedTool(t *testing.T) {
	t.Parallel()

	_, runtime := newHealthTestRuntime(t, &mcp.ServerOptions{}, "available_tool")
	runtime.entrypoint = &AgentDefinition{Name: "test-agent", Tools: ToolAllowlist{Allow: []string{"missing_tool"}}}
	health := runtime.CheckHealth(context.Background())
	if health.OK {
		t.Fatal("CheckHealth() accepted an Engine missing an allowlisted tool")
	}
}

func TestCheckHealthValidatesEveryAgentAllowlist(t *testing.T) {
	t.Parallel()

	_, runtime := newHealthTestRuntime(t, &mcp.ServerOptions{}, "available_tool")
	runtime.config.Deployment = &Deployment{Spec: DeploymentSpec{
		Agents: []AgentDefinition{
			{Name: "entrypoint", Tools: ToolAllowlist{Allow: []string{"available_tool"}}},
			{Name: "worker", Tools: ToolAllowlist{Allow: []string{"missing_tool"}}},
		},
	}}
	health := runtime.CheckHealth(context.Background())
	if health.OK {
		t.Fatal("CheckHealth() validated only the entrypoint allowlist")
	}
	if !strings.Contains(health.Detail, `agent "worker"`) ||
		!strings.Contains(health.Detail, `tool "missing_tool"`) {
		t.Fatalf("health detail = %q, want missing agent and tool", health.Detail)
	}
}
