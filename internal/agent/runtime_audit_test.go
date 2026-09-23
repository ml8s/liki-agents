package agent_test

import (
	"testing"

	"github.com/liki/liki-agent/internal/agent"
)

func newAuditRuntime(t *testing.T) *agent.Runtime {
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

func TestExternalAuditRunLifecycleIsExclusive(t *testing.T) {
	runtime := newAuditRuntime(t)
	scope := agent.AuditRunScope{RunID: "run_1", ThreadID: "thread_1", UserID: "user_1", Product: "liki"}
	if err := runtime.BeginAuditRun("session_1", scope); err != nil {
		t.Fatalf("BeginAuditRun() error = %v", err)
	}
	if err := runtime.BeginAuditRun("session_1", scope); err == nil {
		t.Fatal("second BeginAuditRun unexpectedly succeeded")
	}
	runtime.EndAuditRun("session_1", nil)
	if err := runtime.BeginAuditRun("session_1", scope); err != nil {
		t.Fatalf("BeginAuditRun after EndAuditRun() error = %v", err)
	}
}
