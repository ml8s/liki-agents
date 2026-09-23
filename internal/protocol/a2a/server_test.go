package a2a_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/liki/liki-agent/internal/agent"
	"github.com/liki/liki-agent/internal/platform/buildinfo"
	"github.com/liki/liki-agent/internal/protocol/a2a"
)

func testRuntime(t *testing.T) *agent.Runtime {
	t.Helper()
	runtime, err := agent.NewRuntime(agent.Config{
		Model:           "test-model",
		ModelAPIKey:     "test-key",
		AllowedTools:    []string{"bazi_chart"},
		EngineMCPURL:    "http://127.0.0.1:1/mcp",
		PromptVersion:   "test-prompt",
		PolicyVersion:   "test-policy",
		GraphVersion:    "test-graph",
		ContractVersion: "test-contract",
	})
	if err != nil {
		t.Fatalf("NewRuntime() error = %v", err)
	}
	return runtime
}

func TestAgentCardDeclaresStandardJSONRPCBinding(t *testing.T) {
	publicURL, err := url.Parse("https://agent.internal")
	if err != nil {
		t.Fatal(err)
	}
	server, err := a2a.New(testRuntime(t), a2a.Config{PublicURL: publicURL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	card := server.AgentCard()
	if card.Name != "chief_analyst" || card.Version != buildinfo.Version {
		t.Fatalf("card identity = %q/%q", card.Name, card.Version)
	}
	if len(card.SupportedInterfaces) != 1 || card.SupportedInterfaces[0].URL != "https://agent.internal/a2a" {
		t.Fatalf("supported interfaces = %+v", card.SupportedInterfaces)
	}
	if !card.Capabilities.Streaming {
		t.Fatal("A2A streaming capability was not declared")
	}
	if _, ok := card.SecuritySchemes["liki_service_bearer"]; !ok {
		t.Fatal("A2A bearer security scheme was not declared")
	}
	if len(card.SecurityRequirements) != 1 {
		t.Fatalf("security requirements = %+v", card.SecurityRequirements)
	}
}

func TestWellKnownCardEndpoint(t *testing.T) {
	publicURL, err := url.Parse("https://agent.internal")
	if err != nil {
		t.Fatal(err)
	}
	server, err := a2a.New(testRuntime(t), a2a.Config{PublicURL: publicURL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content type = %q", got)
	}
	if got := response.Body.String(); len(got) == 0 || got[0] != '{' {
		t.Fatalf("agent card body = %q", got)
	}
}
