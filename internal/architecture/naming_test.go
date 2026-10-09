package architecture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageNamesAreStable(t *testing.T) {
	t.Parallel()
	root := filepath.Join(moduleRoot(t), "internal")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read internal: %v", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.ContainsAny(name, " _-") {
			t.Errorf("package directory %q contains non-Go identifier characters", name)
		}
	}
}
