package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWriteComplete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := AtomicWrite(path, []byte("auth_key: supersecret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "supersecret") {
		t.Fatalf("unexpected content %q", got)
	}
	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}
