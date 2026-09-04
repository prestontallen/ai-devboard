package devboard

import (
	"os"
	"path/filepath"
	"strings"
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

func TestFindAcrossRepos(t *testing.T) {
	dir := withDataDir(t)
	seed(t, dir, "repo-b", "tkt-6", "schema: 1\n")
	p, err := Find("tkt-6")
	if err != nil || !strings.HasSuffix(p, filepath.Join("repo-b", "tkt-6.yaml")) {
		t.Fatalf("Find = %q, %v", p, err)
	}
	p, err = Find("absent")
	if err != nil || p != "" {
		t.Fatalf("Find(absent) = %q, %v", p, err)
	}
}
