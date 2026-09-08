package storepath

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDirIsASiblingNotAChild is the whole point of this package. If the
// store directory is ever inside the corpus root, adopt's census refuses,
// its snapshot copies a live database, and its restore deletes one.
func TestDirIsASiblingNotAChild(t *testing.T) {
	root := "/home/someone/.local/share/worklog"
	dir := Dir(root)

	if dir != "/home/someone/.local/share/worklog-store" {
		t.Errorf("Dir = %q, want the -store sibling", dir)
	}
	if strings.HasPrefix(dir, root+string(filepath.Separator)) {
		t.Errorf("Dir %q is INSIDE the corpus root %q", dir, root)
	}
	if DB(root) != filepath.Join(dir, "worklog.db") {
		t.Errorf("DB = %q, want the database inside %q", DB(root), dir)
	}
}

// TestTrailingSeparatorDoesNotProduceANestedDir guards the obvious typo:
// a root with a trailing slash must not yield "worklog/-store".
func TestTrailingSeparatorDoesNotProduceANestedDir(t *testing.T) {
	if got, want := Dir("/tmp/wl/"), "/tmp/wl-store"; got != want {
		t.Errorf("Dir with a trailing separator = %q, want %q", got, want)
	}
}

// TestDistinctRootsGetDistinctStores is what makes test isolation work:
// two scratch corpora must never share one database.
func TestDistinctRootsGetDistinctStores(t *testing.T) {
	if Dir("/tmp/a") == Dir("/tmp/b") {
		t.Error("two different corpus roots resolved to the same store directory")
	}
}
