package adopt

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWritePathCannotReachAdoption is the seam guard.
//
// Adoption rewrites and DELETES live files. The one thing that must never
// happen is a write verb triggering it as a side effect. The shape that
// once threatened this was the shadow-sync hook, which ran a full corpus
// re-derivation on every write; it is gone now, but the hazard is
// structural rather than tied to any one package.
//
// Enforced structurally: no package under internal/ may import
// internal/adopt. Only internal/cli, the composition root, may.
//
// This walks every package rather than a hardcoded list, because the list
// version broke the moment packages were deleted — and a guard whose
// failure mode is "the directory is gone" is a guard that stops guarding
// exactly when the tree is being restructured, which is when it is needed
// most.
func TestWritePathCannotReachAdoption(t *testing.T) {
	const root = ".."
	allowed := map[string]bool{
		"cli":   true, // the composition root
		"adopt": true, // itself
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, pkg := range entries {
		if !pkg.IsDir() || allowed[pkg.Name()] {
			continue
		}
		dir := filepath.Join(root, pkg.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, e := range files {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			checked++
			for _, imp := range f.Imports {
				if strings.Contains(imp.Path.Value, "internal/adopt") {
					t.Errorf("%s imports internal/adopt; adoption must never be reachable from a write", path)
				}
			}
		}
	}
	// A walk that silently checked nothing would pass forever.
	if checked < 10 {
		t.Fatalf("only %d files checked; the walk is not finding the tree", checked)
	}
}
