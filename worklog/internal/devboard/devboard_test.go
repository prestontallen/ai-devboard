package devboard

import (
	"os"
	"path/filepath"
	"testing"
)

func withDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DEVBOARD_DATA", dir)
	return dir
}

func seed(t *testing.T, dir, repo, slug, content string) string {
	t.Helper()
	p := filepath.Join(dir, repo, slug+".yaml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}
