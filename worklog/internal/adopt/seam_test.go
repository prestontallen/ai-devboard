package adopt

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
)

// TestWritePathCannotReachAdoption is the seam guard for criterion 16.
//
// Adoption rewrites and DELETES live files. The one thing that must never
// happen is a write verb triggering it as a side effect — which is exactly
// the shape storesync already has, since WarnAfterWrite calls migrate.Run
// on every write when WORKLOG_STORE_SYNC is set. If adoption ever became
// reachable from there, every note would re-render and prune the corpus.
//
// Enforced structurally rather than by behaviour: no package on the write
// path may import internal/adopt. Only internal/cli, the composition root,
// may.
func TestWritePathCannotReachAdoption(t *testing.T) {
	for _, dir := range []string{"../storesync", "../migrate", "../projection", "../convert", "../verify", "../store"} {
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
				if strings.Contains(imp.Path.Value, "internal/adopt") {
					t.Errorf("%s imports internal/adopt; adoption must never be reachable from a write", path)
				}
			}
		}
	}
}

// TestMigrateHasNoAdoptionKnob: adoption is a separate command, not a flag
// on migrate. migrate.Options gaining a render or adopt field is how this
// would leak into storesync's per-write migrate.Run call.
func TestMigrateHasNoAdoptionKnob(t *testing.T) {
	rt := reflect.TypeOf(migrate.Options{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.ToLower(rt.Field(i).Name)
		if strings.Contains(name, "render") || strings.Contains(name, "adopt") || strings.Contains(name, "apply") {
			t.Errorf("migrate.Options has field %q; adoption must stay out of the per-write migrate path", rt.Field(i).Name)
		}
	}
}
