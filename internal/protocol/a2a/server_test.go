package a2a_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	a2atypes "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/ml8s/liki-agents/internal/agent"
	"github.com/ml8s/liki-agents/internal/audit"
	"github.com/ml8s/liki-agents/internal/protocol/a2a"
	"github.com/ml8s/liki-agents/internal/testagent"
)

type testAuditRecorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *testAuditRecorder) Record(_ context.Context, event *audit.Event) error {
	if event == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, *event)
	return nil
}

func testRuntime(t *testing.T) *agent.Runtime {
	return testRuntimeWithDeployment(t, testagent.Deployment(t))
}

func testRuntimeWithDeployment(t *testing.T, deployment *agent.Deployment) *agent.Runtime {
	t.Helper()
	runtime, err := agent.NewRuntime(agent.Config{
		Model:            "test-model",
		ModelAPIKey:      "test-key",
		Deployment:       deployment,
		StructuredOutput: "json_schema",
		EngineMCPURL:     "http://127.0.0.1:1/mcp",
		GraphVersion:     "test-graph",
		ContractVersion:  "test-contract",
		AuditRecorder:    &testAuditRecorder{},
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
	response := httptest.NewRecorder()
	server.AgentCardHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("Agent Card status = %d, body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Agent Card content type = %q", got)
	}
	var card a2atypes.AgentCard
	if err := json.Unmarshal(response.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode Agent Card: %v", err)
	}
	if card.Name != "main" || card.Version != "1.0.0" {
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
	if len(card.Skills) != 1 || card.Skills[0].ID != "main" {
		t.Fatalf("skills = %+v, want one entrypoint skill", card.Skills)
	}
	if !contains(card.Skills[0].Tags, "entrypoint") {
		t.Fatalf("entrypoint skill tags = %+v", card.Skills[0].Tags)
	}
	if strings.Contains(response.Body.String(), "You are a generic adapter test agent.") {
		t.Fatal("Agent Card leaked Agent instruction")
	}
	if len(card.Capabilities.Extensions) != 1 ||
		card.Capabilities.Extensions[0].URI != "https://liki.hk/contracts/agent-deployment-v1" {
		t.Fatalf("deployment extension = %+v", card.Capabilities.Extensions)
	}
	if card.Capabilities.Extensions[0].Params["digest"] == "" {
		t.Fatalf("deployment digest = %#v, want non-empty", card.Capabilities.Extensions[0].Params["digest"])
	}
}

func TestAgentCardDescribesSubAgentsWithoutInstructions(t *testing.T) {
	deployment := testagent.Deployment(t)
	deployment.Spec.Agents[0].Name = "coordinator"
	deployment.Spec.Agents[0].SubAgents = []agent.AgentReference{{Name: "worker"}}
	worker := deployment.Spec.Agents[0]
	worker.Name = "worker"
	worker.Description = "generic worker"
	worker.Mode = agent.AgentModeTask
	worker.SubAgents = nil
	worker.Tools = agent.ToolAllowlist{Allow: []string{"test_tool"}}
	deployment.Spec.Agents = append(deployment.Spec.Agents, worker)
	if err := deployment.Validate(); err != nil {
		t.Fatalf("validate deployment: %v", err)
	}

	publicURL, err := url.Parse("https://agent.internal")
	if err != nil {
		t.Fatal(err)
	}
	server, err := a2a.New(testRuntimeWithDeployment(t, deployment), a2a.Config{PublicURL: publicURL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	server.AgentCardHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/.well-known/agent-card.json", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("Agent Card status = %d, body = %s", response.Code, response.Body.String())
	}
	var card a2atypes.AgentCard
	if err := json.Unmarshal(response.Body.Bytes(), &card); err != nil {
		t.Fatalf("decode Agent Card: %v", err)
	}
	if len(card.Skills) != 2 {
		t.Fatalf("skills = %+v, want entrypoint and sub-agent", card.Skills)
	}
	bySkillID := map[string]a2atypes.AgentSkill{}
	for _, skill := range card.Skills {
		bySkillID[skill.ID] = skill
	}
	coordinator, ok := bySkillID["coordinator"]
	if !ok || !contains(coordinator.Tags, "entrypoint") {
		t.Fatalf("coordinator skill = %+v", coordinator)
	}
	workerSkill, ok := bySkillID["worker"]
	if !ok || !contains(workerSkill.Tags, "tool:test_tool") {
		t.Fatalf("worker skill = %+v", workerSkill)
	}
	if strings.Contains(response.Body.String(), "You are a generic adapter test agent.") {
		t.Fatal("Agent Card leaked Agent instruction")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
