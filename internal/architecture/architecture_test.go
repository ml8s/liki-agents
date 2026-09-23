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
	root := filepath.Join(moduleRoot(t), "internal", "domain")
	for path, imports := range importsUnder(t, root) {
		if imported := forbidden(imports,
			"github.com/liki/liki-agent/internal/adapters",
			"github.com/liki/liki-agent/internal/agent",
			"github.com/liki/liki-agent/internal/protocol",
			"github.com/liki/liki-agent/internal/platform",
			"github.com/liki/liki-agent/internal/ports",
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

func TestNoSourceFileWritesSecrets(t *testing.T) {
	root := moduleRoot(t)
	for path, imports := range importsUnder(t, root) {
		if strings.Contains(path, "architecture_test.go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		if strings.Contains(text, "os.Setenv(") || strings.Contains(text, "os.WriteFile(") {
			t.Errorf("%s mutates process environment or files", path)
		}
		_ = imports
	}
}

func TestLegacyRuntimeAbstractionsAreAbsent(t *testing.T) {
	root := moduleRoot(t)
	banned := []string{
		"ThreadRepository",
		"MessageRepository",
		"RunRepository",
		"EventRepository",
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

func TestProtocolAdaptersDoNotCrossImport(t *testing.T) {
	root := moduleRoot(t)
	for path, imports := range importsUnder(t, root) {
		if strings.HasPrefix(path, filepath.Join("internal", "protocol", "a2a")) &&
			forbidden(imports, "github.com/liki/liki-agent/internal/protocol/agui") != "" {
			t.Errorf("%s imports AG-UI", path)
		}
		if strings.HasPrefix(path, filepath.Join("internal", "protocol", "agui")) &&
			forbidden(imports, "github.com/liki/liki-agent/internal/protocol/a2a") != "" {
			t.Errorf("%s imports A2A", path)
		}
		if strings.HasPrefix(path, filepath.Join("internal", "agent")) &&
			forbidden(imports, "github.com/liki/liki-agent/internal/protocol") != "" {
			t.Errorf("%s protocol import violates inward dependency direction", path)
		}
	}
}

func TestErrorCodesAreConstants(t *testing.T) {
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
