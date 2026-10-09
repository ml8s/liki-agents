// These are static contract-regression tests: they assert pinned artifacts/tooling, not runtime behavior.
package pipeline_test

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// The pre-push gate is deliberately limited to local static checks and
// isolated tests. External Engine and real-LLM checks require deployed
// credentials and must remain explicit targets.
func TestMakeGateRunsOnlyLocalChecksAndTests(t *testing.T) {
	command := exec.Command("make", "--no-print-directory", "-n", "gate")
	// Tests execute in the package directory, while the Makefile is at the
	// module root.
	command.Dir = "../.."
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("make -n gate: %v\n%s", err, output)
	}
	dryRun := string(output)
	if !strings.Contains(dryRun, "go vet ./...") {
		t.Fatal("gate does not run static vet checks")
	}
	if !strings.Contains(dryRun, "go test -race") {
		t.Fatal("gate does not run isolated race tests")
	}
	if strings.Contains(dryRun, "./tests/contract") {
		t.Fatal("gate unexpectedly depends on a deployed Engine contract")
	}
	if strings.Contains(dryRun, "go build") {
		t.Fatal("gate unexpectedly includes build; keep build in CI and release checks")
	}
}

func TestGitHubWorkflowExecutesGateAndBuild(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read GitHub workflow: %v", err)
	}
	workflow := string(raw)
	if !strings.Contains(workflow, "run: make gate") {
		t.Fatal("CI workflow does not run make gate")
	}
	if !strings.Contains(workflow, "run: make build") {
		t.Fatal("CI workflow does not build the production binary")
	}
}

func TestGitHubWorkflowInstallsNodeDependenciesBeforeGate(t *testing.T) {
	raw, err := os.ReadFile("../../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read GitHub workflow: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	installIndex := slices.Index(lines, "      - name: Install Node dependencies")
	gateIndex := slices.Index(lines, "      - name: Run pre-push gate")
	if installIndex < 0 || gateIndex < 0 {
		t.Fatalf("Node install or gate step missing; install=%d gate=%d", installIndex, gateIndex)
	}
	if installIndex > gateIndex {
		t.Fatal("Node dependencies must be installed before the gate")
	}
	if !slices.Contains(lines[installIndex:gateIndex], "        run: npm ci") {
		t.Fatal("Node install step must use reproducible npm ci")
	}
}
