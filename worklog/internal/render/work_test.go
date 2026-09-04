package render

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomic(t *testing.T) {
	tmpdir := t.TempDir()
	dst := filepath.Join(tmpdir, "x.md")
	if err := WriteAtomic(dst, []string{"hello", "world"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\nworld\n" {
		t.Errorf("got %q", string(got))
	}
}
