package architecture_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate architecture test")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}

func importsUnder(t *testing.T, root string) map[string][]string {
	t.Helper()
	result := make(map[string][]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		imports := make([]string, 0, len(file.Imports))
		for _, imported := range file.Imports {
			imports = append(imports, strings.Trim(imported.Path.Value, `"`))
		}
		result[relative] = imports
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return result
}

func forbidden(list []string, prefixes ...string) string {
	for _, imported := range list {
		for _, prefix := range prefixes {
			if imported == prefix || strings.HasPrefix(imported, prefix+"/") {
				return imported
			}
		}
	}
	return ""
}

func TestDomainStaysPure(t *testing.T) {
	t.Parallel()
	root := filepath.Join(moduleRoot(t), "internal", "domain")
	for path, imports := range importsUnder(t, root) {
		if imported := forbidden(imports,
			"github.com/ml8s/liki-agents/internal/adapters",
			"github.com/ml8s/liki-agents/internal/agent",
			"github.com/ml8s/liki-agents/internal/protocol",
			"github.com/ml8s/liki-agents/internal/platform",
			"github.com/ml8s/liki-agents/internal/ports",
			"google.golang.org/adk",
			"google.golang.org/genai",
			"net/http",
			"database/sql",
		); imported != "" {
			t.Errorf("%s imports %s", path, imported)
		}
	}
}

func TestADKStaysInRuntimeAndProtocolAdapters(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for path, imports := range importsUnder(t, root) {
		protocolAdapter := false
		for _, allowed := range []string{"a2a", "agui"} {
			if path == filepath.Join("internal", "protocol", allowed) || strings.HasPrefix(path, filepath.Join("internal", "protocol", allowed)+string(filepath.Separator)) {
				protocolAdapter = true
			}
		}
		if path == filepath.Join("internal", "agent") || strings.HasPrefix(path, filepath.Join("internal", "agent")+string(filepath.Separator)) || protocolAdapter {
			continue
		}
		if imported := forbidden(imports, "google.golang.org/adk", "google.golang.org/genai"); imported != "" {
			t.Errorf("%s imports %s outside the agent/protocol boundary", path, imported)
		}
	}
}

func TestProductionSourceDoesNotMutateHostState(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		if strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, "architecture_test.go") || strings.Contains(path, filepath.Join("internal", "testagent")) {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		if strings.Contains(text, "os.Setenv(") || strings.Contains(text, "os.WriteFile(") {
			t.Errorf("%s mutates process environment or files", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source: %v", err)
	}
}

func TestLegacyRuntimeAbstractionsAreAbsent(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	banned := []string{
		"ThreadRepository",
		"MessageRepository",
		"RunRepository",
		"IdempotencyRepository",
		"EventSink",
		"/v1/threads",
		"/v1/runs",
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		if strings.HasSuffix(path, "architecture_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, value := range banned {
			if strings.Contains(text, value) {
				t.Errorf("%s contains legacy runtime concept %q", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source: %v", err)
	}
}

func TestDatabaseTechnologyStaysInAuditSQLite(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for path, imports := range importsUnder(t, root) {
		if strings.HasPrefix(path, filepath.Join("internal", "audit", "sqlite")) {
			continue
		}
		if imported := forbidden(imports, "gorm.io/gorm", "github.com/glebarez/sqlite", "modernc.org/sqlite"); imported != "" {
			t.Errorf("%s imports database technology %s outside audit/sqlite", path, imported)
		}
	}
}

func TestRuntimeDoesNotUseADKDemoLogging(t *testing.T) {
	t.Parallel()
	for path, imports := range importsUnder(t, filepath.Join(moduleRoot(t), "internal")) {
		if imported := forbidden(imports, "google.golang.org/adk/v2/plugin/loggingplugin"); imported != "" {
			t.Errorf("%s imports unsafe demo logger %s", path, imported)
		}
	}
}

func TestADKLauncherIsNotAPublicAPI(t *testing.T) {
	t.Parallel()
	for path, imports := range importsUnder(t, moduleRoot(t)) {
		if imported := forbidden(imports,
			"google.golang.org/adk/v2/cmd/launcher",
			"google.golang.org/adk/v2/server/adkrest",
		); imported != "" {
			t.Errorf("%s imports ADK Launcher REST machinery %s", path, imported)
		}
	}
}

func TestAgentCardDoesNotUseInstructionDerivedSkills(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), "BuildAgentSkills") {
			t.Errorf("%s uses ADK instruction-derived Agent Card skills", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source: %v", err)
	}
}

func TestProtocolAdaptersDoNotCrossImport(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	for path, imports := range importsUnder(t, root) {
		if strings.HasPrefix(path, filepath.Join("internal", "protocol", "a2a")) &&
			forbidden(imports, "github.com/ml8s/liki-agents/internal/protocol/agui") != "" {
			t.Errorf("%s imports AG-UI", path)
		}
		if strings.HasPrefix(path, filepath.Join("internal", "protocol", "agui")) &&
			forbidden(imports, "github.com/ml8s/liki-agents/internal/protocol/a2a") != "" {
			t.Errorf("%s imports A2A", path)
		}
		if strings.HasPrefix(path, filepath.Join("internal", "agent")) &&
			forbidden(imports, "github.com/ml8s/liki-agents/internal/protocol") != "" {
			t.Errorf("%s protocol import violates inward dependency direction", path)
		}
	}
}

func TestErrorCodesAreConstants(t *testing.T) {
	t.Parallel()
	root := moduleRoot(t)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		if strings.HasSuffix(path, "_test.go") || strings.Contains(path, "error_codes.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, pattern := range []string{
			`NewError("`, `c.ErrorCode = "`,
		} {
			if strings.Contains(text, pattern) {
				t.Errorf("%s uses bare string error code with pattern %q; use domain.Code* constants", path, pattern)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk source: %v", err)
	}
}

func TestAgentRuntimeHasNoBuiltInDomainWorkflow(t *testing.T) {
	t.Parallel()
	root := filepath.Join(moduleRoot(t), "internal", "agent")
	banned := []string{
		"ExpertOpinion",
		"ProductProfile",
		"structuredOutputSchema",
		"chief_analyst",
		"destiny-analysis expert",
	}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return walkErr
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		text := string(data)
		for _, value := range banned {
			if strings.Contains(text, value) {
				t.Errorf("%s contains built-in domain workflow concept %q", path, value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk agent runtime: %v", err)
	}
}
