package store

// The swappability boundary, enforced: no consumer package may import a
// Store implementation. Only test files (which parameterize over both
// implementations) and the eventual composition root wire one in.

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsumersImportOnlyTheInterface(t *testing.T) {
	consumers := []string{"../convert", "../projection"}
	banned := []string{
		"internal/store/sqlitestore",
		"internal/store/memstore",
	}
	for _, dir := range consumers {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				for _, b := range banned {
					if strings.Contains(imp.Path.Value, b) {
						t.Errorf("%s imports %s — consumers program against the Store interface only", path, imp.Path.Value)
					}
				}
			}
		}
	}
}
