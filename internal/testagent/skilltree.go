package testagent

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMinimalSkill creates a valid ADK skill rootFS for runtime preload.
func writeMinimalSkill(t testing.TB, root string) {
	t.Helper()
	dir := filepath.Join(root, "testskill")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0o755); err != nil {
		t.Fatal(err)
	}
	skill := `---
name: testskill
description: Minimal adapter-test skill providing one reference card for skilltoolset preload coverage.
---

# testskill

See references/card.md.`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "card.md"), []byte("# card\n\nBody."), 0o600); err != nil {
		t.Fatal(err)
	}
}
