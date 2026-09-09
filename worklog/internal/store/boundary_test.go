package store

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// constructors are the calls that bring a concrete Store into existence.
var constructors = map[string]string{
	"sqlitestore.Open": "the durable adapter",
	"memstore.New":     "the in-memory adapter",
}

// allowed names the production files permitted to construct a store, and why.
//
// The second entry is a deliberate, permanent exception rather than a site
// waiting to be collapsed: `store relocate` opens a database it has just
// copied, to prove the copy is a real database rather than bytes of the right
// length, and removes it if not. That is a file-copy verification. An adapter
// that is not file-backed has nothing for it to do, so routing it through the
// opener would be a false uniformity — the call would still be sqlite-shaped,
// just further away from the reason it exists.
var allowed = map[string]string{
	"internal/cli/storeopen.go": "the single construction path",
	"internal/cli/store.go":     "store relocate: verifies a copied database, inherently file-backed",
}

// TestStoreIsConstructedInOnePlace is the guard behind the epic's claim that a
// second adapter is a drop-in.
//
// It counts CONSTRUCTION rather than banning imports, and that shape was
// chosen against evidence. Every construction site lives in internal/cli, the
// composition root — a fact adopt's seam test already encodes — so a guard
// that banned implementation imports outside the root would have left all of
// them legal and proved nothing at all.
//
// What replaced it also matters. The previous version scanned a hardcoded list
// of two directories and had not been touched since the commit that created
// it: exactly the rot adopt's seam test was rewritten to avoid, where a guard
// stops guarding at the moment the tree is restructured. This walks instead,
// and fails when it finds nothing to inspect.
//
// _test.go files are exempt, and that is load-bearing rather than laziness:
// around twenty of them construct implementations directly, because
// parameterising a test over BOTH implementations is how swappability is
// actually proven. Tightening this would delete the proof.
func TestStoreIsConstructedInOnePlace(t *testing.T) {
	root := "../.."
	var checked int
	var offenders []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable corners are not this test's business
		}
		if d.IsDir() {
			// Worktrees hold a second full copy of the tree; counting them
			// would double every site and fail for the wrong reason.
			if n := d.Name(); n == ".git" || n == "worktrees" || n == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if perr != nil {
			return nil
		}
		checked++

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			name := pkg.Name + "." + sel.Sel.Name
			if _, isCtor := constructors[name]; !isCtor {
				return true
			}
			if _, ok := allowed[rel]; ok {
				return true
			}
			offenders = append(offenders, rel+" calls "+name)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// A walk that silently inspected nothing would pass forever.
	if checked < 50 {
		t.Fatalf("only %d production files parsed; the walk is not finding the tree", checked)
	}
	for _, o := range offenders {
		t.Errorf("%s — store construction belongs in internal/cli/storeopen.go, so a second adapter is one edit rather than several", o)
	}
}

// TestConsumersImportOnlyTheInterface keeps the older, narrower promise: the
// packages that merely USE a store must not name an implementation at all.
// Counting construction would not catch an import kept for a type assertion.
func TestConsumersImportOnlyTheInterface(t *testing.T) {
	banned := []string{"internal/store/sqlitestore", "internal/store/memstore"}
	root := "../.."
	var checked int

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "worktrees", "node_modules", "cli", "sqlitestore", "memstore", "storetest":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if perr != nil {
			return nil
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		for _, imp := range f.Imports {
			for _, b := range banned {
				if strings.Contains(imp.Path.Value, b) {
					t.Errorf("%s imports %s — consumers program against the Store interface only",
						filepath.ToSlash(rel), b)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if checked < 40 {
		t.Fatalf("only %d files parsed; the walk is not finding the tree", checked)
	}
}
